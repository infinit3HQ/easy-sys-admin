package fleet

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	exposureEvery = time.Hour
	// exposurePerCycle caps how many servers collect reports in one poll
	// cycle. The access report can read a day's SSH journal, so doing it
	// for the whole fleet at once would starve status polls.
	exposurePerCycle = 8
)

// Exposure is the small summary stored for the board. It has no user
// list, no key material, and no login addresses.
type Exposure struct {
	CheckedAt            time.Time `json:"checkedAt"`
	Ready                bool      `json:"ready"`
	PublicPorts          []string  `json:"publicPorts"`
	PasswordAuth         bool      `json:"passwordAuth"`
	PermitRootLogin      string    `json:"permitRootLogin"`
	PermitEmptyPasswords bool      `json:"permitEmptyPasswords"`
	PubkeyAuth           bool      `json:"pubkeyAuth"`
	SSHRead              bool      `json:"sshRead"`
	Firewall             string    `json:"firewall"`
	ExtraRoot            int       `json:"extraRoot"`
	NoPasswordAll        int       `json:"noPasswordAll"`
}

func summarizeExposure(access proto.AccessReport, sec proto.SecurityReport, at time.Time) Exposure {
	ex := Exposure{
		CheckedAt: at, Ready: access.Revision >= proto.ReportRevision && sec.Revision >= proto.ReportRevision,
		PublicPorts: []string{},
	}
	if !ex.Ready {
		return ex
	}
	seen := map[string]bool{}
	for _, l := range sec.Listening {
		if l.Exposure != "all" {
			continue
		}
		p := l.Proto
		switch {
		case strings.HasPrefix(p, "tcp"):
			p = "tcp"
		case strings.HasPrefix(p, "udp"):
			p = "udp"
		}
		key := strconv.Itoa(l.Port) + "/" + p
		if seen[key] {
			continue
		}
		seen[key] = true
		ex.PublicPorts = append(ex.PublicPorts, key)
	}
	slices.Sort(ex.PublicPorts)
	ex.PasswordAuth = access.PasswordAuth
	ex.PermitRootLogin = access.PermitRootLogin
	ex.PermitEmptyPasswords = access.PermitEmptyPasswords
	ex.PubkeyAuth = access.PubkeyAuth
	ex.SSHRead = access.SSHRead
	ex.Firewall = sec.Firewall
	for _, u := range access.Users {
		if u.UID == 0 && u.Name != "root" {
			ex.ExtraRoot++
		}
	}
	for _, r := range access.SudoRules {
		if r.Unrestricted && r.NoPassword {
			ex.NoPasswordAll++
		}
	}
	return ex
}

func exposureChanged(prev, next Exposure) bool {
	if !prev.Ready || !next.Ready {
		return false
	}
	return prev.PasswordAuth != next.PasswordAuth ||
		prev.PermitRootLogin != next.PermitRootLogin ||
		prev.PermitEmptyPasswords != next.PermitEmptyPasswords ||
		prev.PubkeyAuth != next.PubkeyAuth ||
		prev.SSHRead != next.SSHRead ||
		prev.Firewall != next.Firewall ||
		prev.ExtraRoot != next.ExtraRoot ||
		prev.NoPasswordAll != next.NoPasswordAll ||
		!slices.Equal(prev.PublicPorts, next.PublicPorts)
}

func exposureConditions(ex Exposure) []condition {
	if !ex.Ready {
		return nil
	}
	var out []condition
	add := func(subject, sev, msg string) {
		out = append(out, condition{kind: AlertExposure, subject: subject, severity: sev, message: msg})
	}
	if ex.PasswordAuth {
		add("password", "warning", "SSH password login is allowed")
	}
	if ex.PermitEmptyPasswords {
		add("empty-password", "critical", "SSH allows empty passwords")
	}
	if ex.PermitRootLogin == "yes" {
		add("root-login", "warning", "SSH allows root to log in with a password")
	}
	if ex.Firewall == "none" {
		add("firewall", "warning", "no firewall is active")
	}
	if ex.ExtraRoot > 0 {
		add("extra-root", "critical", "a login account other than root has uid 0")
	}
	if ex.NoPasswordAll > 0 {
		add("nopasswd", "warning", "a sudo rule grants NOPASSWD ALL")
	}
	return out
}

// maybeExposure collects the summary when it is due. budgeted is true
// during a fleet poll, which only starts a few of these per cycle.
func (s *Service) maybeExposure(ctx context.Context, srv store.Server, state *store.ServerState, budgeted bool) {
	if state.ExposureAt != nil && s.now().Sub(*state.ExposureAt) < exposureEvery {
		return
	}
	if budgeted && !s.takeExposureSlot() {
		return
	}
	var prev Exposure
	_ = state.Exposure.Decode(&prev)
	ex, ok := s.collectExposure(ctx, srv)
	now := s.now()
	if !ok {
		// A cancelled poll cycle should try again next minute. Any other
		// failure waits an hour so a slow server cannot crowd out status polls.
		if ctx.Err() == nil {
			s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", srv.ID).
				Update("exposure_at", now)
			state.ExposureAt = &now
		}
		return
	}
	if exposureChanged(prev, ex) {
		s.audit(ctx, nil, System(srv.AccountID), "server.exposure", srv.Name, map[string]any{
			"publicPorts": ex.PublicPorts, "passwordAuth": ex.PasswordAuth,
			"permitRootLogin": ex.PermitRootLogin, "permitEmptyPasswords": ex.PermitEmptyPasswords,
			"firewall": ex.Firewall, "extraRoot": ex.ExtraRoot, "noPasswordAll": ex.NoPasswordAll,
		})
	}
	raw := store.MustJSON(ex)
	s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", srv.ID).
		Updates(map[string]any{"exposure": raw, "exposure_at": now})
	state.Exposure, state.ExposureAt = raw, &now
}

func (s *Service) takeExposureSlot() bool {
	for {
		n := s.exposureBudget.Load()
		if n <= 0 {
			return false
		}
		if s.exposureBudget.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

func (s *Service) collectExposure(ctx context.Context, srv store.Server) (Exposure, bool) {
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		s.log.Warn("exposure report", "server_id", srv.ID, "err", err)
		return Exposure{}, false
	}
	var access proto.AccessReport
	cctx, cancel := context.WithTimeout(s.netCtx(ctx), 45*time.Second)
	err = s.pool.Call(cctx, ep, &access, "access")
	cancel()
	if err != nil {
		s.noteExposureErr(ctx, srv, err)
		return Exposure{}, false
	}
	var sec proto.SecurityReport
	cctx, cancel = context.WithTimeout(s.netCtx(ctx), 30*time.Second)
	err = s.pool.Call(cctx, ep, &sec, "security")
	cancel()
	if err != nil {
		s.noteExposureErr(ctx, srv, err)
		return Exposure{}, false
	}
	return summarizeExposure(access, sec, s.now()), true
}

func (s *Service) noteExposureErr(ctx context.Context, srv store.Server, err error) {
	if ctx.Err() != nil {
		return
	}
	var mismatch *remote.HostKeyMismatchError
	if errors.As(err, &mismatch) {
		s.recordMismatch(ctx, srv.ID, mismatch)
		return
	}
	s.log.Warn("exposure report", "server_id", srv.ID, "err", err)
}
