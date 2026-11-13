package fleet

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// HealthCheck is one check frozen onto an update run. It dials the enrolled
// server, or asks the helper about one unit or container. It never takes a
// host name from the operator.
type HealthCheck struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Blocking  bool   `json:"blocking"`
	Port      int    `json:"port,omitempty"`
	TLS       bool   `json:"tls,omitempty"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Container string `json:"container,omitempty"`
}

// CheckResult is one pass or fail. Detail is a short reason, never a body.
type CheckResult struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Phase    string `json:"phase"`
	State    string `json:"state"`
	Detail   string `json:"detail,omitempty"`
	Blocking bool   `json:"blocking"`
	// Overridden is set on a failed check before the upgrade that a
	// person let the run pass, with a reason, for this run only.
	Overridden bool `json:"overridden,omitempty"`
}

// CheckOverride lets named blocking checks fail before the upgrade for
// one run. A failure after the upgrade still stops the server.
type CheckOverride struct {
	Checks []string `json:"checks"`
	Reason string   `json:"reason"`
	By     int64    `json:"by"`
}

// beforeChecks returns the checks for the "before" phase with overridden
// ones made advisory, and marks their failed results.
func beforeChecks(checks []HealthCheck, o *CheckOverride) []HealthCheck {
	if o == nil {
		return checks
	}
	out := make([]HealthCheck, len(checks))
	for i, c := range checks {
		if slices.Contains(o.Checks, c.Name) {
			c.Blocking = false
		}
		out[i] = c
	}
	return out
}

func markOverridden(results []CheckResult, o *CheckOverride) {
	if o == nil {
		return
	}
	for i := range results {
		if results[i].State != "pass" && slices.Contains(o.Checks, results[i].Name) {
			results[i].Overridden, results[i].Blocking = true, true
		}
	}
}

const maxHealthChecks = 8

var (
	checkNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,39}$`)
	checkPathRe = regexp.MustCompile(`^/[A-Za-z0-9/._~-]{0,199}$`)
	checkUnitRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.@-]{0,80}$`)
	checkBoxRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

// validChecks returns a cleaned copy. Nil is an empty list.
func validChecks(in []HealthCheck) ([]HealthCheck, error) {
	if len(in) > maxHealthChecks {
		return nil, invalid("checks", "at most %d checks", maxHealthChecks)
	}
	out := make([]HealthCheck, 0, len(in))
	seen := map[string]bool{}
	for _, c := range in {
		c.Name = strings.Join(strings.Fields(c.Name), " ")
		c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
		c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
		c.Path = strings.TrimSpace(c.Path)
		c.Unit = strings.TrimSpace(c.Unit)
		c.Container = strings.TrimSpace(c.Container)
		if !checkNameRe.MatchString(c.Name) {
			return nil, invalid("checks", "each check needs a name of letters, numbers, spaces, dot, _ or -")
		}
		if seen[c.Name] {
			return nil, invalid("checks", "two checks are named %q", c.Name)
		}
		seen[c.Name] = true
		switch c.Kind {
		case "tcp":
			if c.Port < 1 || c.Port > 65535 {
				return nil, invalid("checks", "%s needs a port from 1 to 65535", c.Name)
			}
			c.Method, c.Path, c.Unit, c.Container, c.TLS = "", "", "", "", false
		case "http":
			if c.Port < 1 || c.Port > 65535 {
				return nil, invalid("checks", "%s needs a port from 1 to 65535", c.Name)
			}
			if c.Method == "" {
				c.Method = http.MethodGet
			}
			if c.Method != http.MethodGet && c.Method != http.MethodHead {
				return nil, invalid("checks", "%s method must be GET or HEAD", c.Name)
			}
			if !checkPathRe.MatchString(c.Path) {
				return nil, invalid("checks", "%s path must start with / and stay on that server", c.Name)
			}
			c.Unit, c.Container = "", ""
		case "unit":
			if !strings.HasSuffix(c.Unit, ".service") {
				c.Unit += ".service"
			}
			if !checkUnitRe.MatchString(strings.TrimSuffix(c.Unit, ".service")) {
				return nil, invalid("checks", "%s is not a service name", c.Name)
			}
			c.Port, c.Method, c.Path, c.Container, c.TLS = 0, "", "", "", false
		case "container":
			if !checkBoxRe.MatchString(c.Container) {
				return nil, invalid("checks", "%s is not a container name", c.Name)
			}
			c.Port, c.Method, c.Path, c.Unit, c.TLS = 0, "", "", "", false
		default:
			return nil, invalid("checks", "%s kind must be tcp, http, unit, or container", c.Name)
		}
		out = append(out, c)
	}
	return out, nil
}

func validCanaries(ids []int64, servers []store.Server) error {
	if len(ids) == 0 {
		return nil
	}
	have := map[int64]bool{}
	for _, srv := range servers {
		have[srv.ID] = true
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if !have[id] {
			return invalid("canaryServerIds", "each canary must be a server in this run")
		}
		if seen[id] {
			return invalid("canaryServerIds", "a server is listed twice")
		}
		seen[id] = true
	}
	return nil
}

// partitionCanaries splits queued indexes into canaries, in the listed
// order, and the rest in their existing order.
func partitionCanaries(items []store.RunServer, queued []int, canaryIDs []int64) (canary, rest []int) {
	if len(canaryIDs) == 0 {
		return nil, queued
	}
	byID := map[int64]int{}
	queuedSet := map[int]bool{}
	for _, i := range queued {
		byID[items[i].ServerID] = i
		queuedSet[i] = true
	}
	used := map[int]bool{}
	for _, id := range canaryIDs {
		i, ok := byID[id]
		if !ok || !queuedSet[i] {
			continue
		}
		canary = append(canary, i)
		used[i] = true
	}
	for _, i := range queued {
		if !used[i] {
			rest = append(rest, i)
		}
	}
	return canary, rest
}

// runHealthChecks dials or asks the helper. A blocking failure returns a
// reason. Advisory failures stay in the results.
// sampleUntil runs check immediately and again after each gap until the
// window ends. The first failing check stops it.
func sampleUntil(window, gap time.Duration, now func() time.Time, sleep func(time.Duration), check func() string) (int, string) {
	if window <= 0 {
		return 0, ""
	}
	if gap < time.Second {
		gap = 15 * time.Second
	}
	deadline := now().Add(window)
	n := 0
	for {
		n++
		if reason := check(); reason != "" {
			return n, reason
		}
		left := deadline.Sub(now())
		if left <= 0 {
			return n, ""
		}
		if left > gap {
			left = gap
		}
		sleep(left)
	}
}

// watchCanaries repeats the run's checks on the canaries before the other
// servers start. It does not start apt again.
func (s *Service) watchCanaries(ctx context.Context, items []store.RunServer, canary []int, o RunOptions) string {
	_, reason := sampleUntil(time.Duration(o.ObserveSeconds)*time.Second, 15*time.Second, time.Now, func(d time.Duration) {
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}, func() string {
		if ctx.Err() != nil {
			return "the run was cancelled"
		}
		for _, i := range canary {
			rs := &items[i]
			if rs.Status != "succeeded" {
				return rs.Server.Name + " did not finish, so the watch cannot pass"
			}
			ep, err := s.endpoint(ctx, rs.Server)
			if err != nil {
				return rs.Server.Name + ": " + err.Error()
			}
			_, block := s.runHealthChecks(ctx, rs.Server, ep, o.Checks, "watch")
			if block != "" {
				return rs.Server.Name + ": " + block
			}
		}
		return ""
	})
	return reason
}

func (s *Service) runHealthChecks(ctx context.Context, srv store.Server, ep remote.Endpoint, checks []HealthCheck, phase string) ([]CheckResult, string) {
	if len(checks) == 0 {
		return nil, ""
	}
	var units []proto.Service
	var boxes []proto.Container
	needUnits, needBoxes := false, false
	for _, c := range checks {
		if c.Kind == "unit" {
			needUnits = true
		}
		if c.Kind == "container" {
			needBoxes = true
		}
	}
	if needUnits {
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := s.pool.Call(cctx, ep, &units, "services")
		cancel()
		if err != nil {
			return nil, "could not list services: " + err.Error()
		}
	}
	if needBoxes {
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := s.pool.Call(cctx, ep, &boxes, "containers")
		cancel()
		if err != nil {
			return nil, "could not list containers: " + err.Error()
		}
	}
	out := make([]CheckResult, 0, len(checks))
	var block string
	for _, c := range checks {
		res := CheckResult{Name: c.Name, Kind: c.Kind, Phase: phase, State: "pass", Blocking: c.Blocking}
		switch c.Kind {
		case "tcp":
			res.Detail = s.checkTCP(ctx, srv.Host, c.Port)
		case "http":
			res.Detail = s.checkHTTP(ctx, srv.Host, c)
		case "unit":
			res.Detail = unitState(units, c.Unit)
		case "container":
			res.Detail = containerState(boxes, c.Container)
		}
		if res.Detail != "" {
			res.State = "fail"
			if c.Blocking && block == "" {
				block = c.Name + ": " + res.Detail
			}
		}
		out = append(out, res)
	}
	return out, block
}

func (s *Service) checkTCP(ctx context.Context, host string, port int) string {
	if host == "" {
		return "server has no address"
	}
	d := netguard.Dialer(s.allowPrivate, net.Dialer{Timeout: 5 * time.Second})
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := d.DialContext(cctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return dialReason(err)
	}
	_ = conn.Close()
	return ""
}

func (s *Service) checkHTTP(ctx context.Context, host string, c HealthCheck) string {
	if host == "" {
		return "server has no address"
	}
	scheme := "http"
	if c.TLS {
		scheme = "https"
	}
	url := scheme + "://" + net.JoinHostPort(host, strconv.Itoa(c.Port)) + c.Path
	dial := netguard.Dialer(s.allowPrivate, net.Dialer{Timeout: 5 * time.Second})
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect refused")
		},
		// One transport per check, for this host's TLS name. Keep-alives are
		// off and idle connections closed, so checks leave no sockets open.
		Transport: &http.Transport{
			DialContext:       dial.DialContext,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host},
			DisableKeepAlives: true,
		},
	}
	defer client.CloseIdleConnections()
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, c.Method, url, nil)
	if err != nil {
		return "bad request"
	}
	resp, err := client.Do(req)
	if err != nil {
		return dialReason(err)
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return ""
}

func dialReason(err error) string {
	if netguard.IsBlocked(err) {
		return "address blocked"
	}
	msg := err.Error()
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return msg
}

func unitState(units []proto.Service, name string) string {
	for _, u := range units {
		if u.Unit != name {
			continue
		}
		if u.Active == "active" && u.Sub != "failed" {
			return ""
		}
		return "active=" + u.Active + " sub=" + u.Sub
	}
	return "not loaded"
}

func containerState(boxes []proto.Container, name string) string {
	for _, c := range boxes {
		if c.Name != name {
			continue
		}
		if c.State != "running" {
			return "state=" + c.State
		}
		if c.Health != "" && c.Health != "healthy" {
			return "health=" + c.Health
		}
		return ""
	}
	return "not found"
}

// checkNames lists check names for the audit log.
func checkNames(checks []HealthCheck) []string {
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.Name)
	}
	return out
}
