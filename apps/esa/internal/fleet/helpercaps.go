package fleet

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	// helperCheckEvery is how often a helper is asked what it can do. A
	// reinstall through esa records it at once; a manual one within a day.
	helperCheckEvery = 24 * time.Hour
	// helperRetryEvery spaces out asking a server that did not answer.
	helperRetryEvery = time.Hour
)

// storedCapabilities decodes what the helper last reported. nil means it
// never listed its actions: the legacy set applies.
func storedCapabilities(srv store.Server) []string {
	if len(srv.HelperCapabilities) == 0 || string(srv.HelperCapabilities) == "null" {
		return nil
	}
	var caps []string
	if err := json.Unmarshal(srv.HelperCapabilities, &caps); err != nil {
		return nil
	}
	if caps == nil {
		caps = []string{}
	}
	return caps
}

// helperCapabilities returns the helper's actions, asking the helper when
// the stored answer is missing or a day old. A failed ask keeps the stored
// answer; with none, only the legacy actions are allowed, so a new action
// is never sent to a helper that has not said it has it.
func (s *Service) helperCapabilities(ctx context.Context, srv store.Server, ep remote.Endpoint) []string {
	now := s.now()
	if srv.HelperCheckedAt != nil && now.Sub(*srv.HelperCheckedAt) < helperCheckEvery {
		return storedCapabilities(srv)
	}
	if last, ok := s.helperTried.Load(srv.ID); ok && now.Sub(last.(time.Time)) < helperRetryEvery {
		return storedCapabilities(srv)
	}
	s.helperTried.Store(srv.ID, now)
	v, err := s.askHelperVersion(ctx, ep)
	if err != nil {
		return storedCapabilities(srv)
	}
	s.recordHelper(ctx, srv.ID, v)
	return v.Capabilities
}

func (s *Service) askHelperVersion(ctx context.Context, ep remote.Endpoint) (proto.VersionInfo, error) {
	var v proto.VersionInfo
	cctx, cancel := context.WithTimeout(s.netCtx(ctx), 30*time.Second)
	defer cancel()
	err := s.pool.Call(cctx, ep, &v, "version")
	return v, err
}

// recordHelper stores what `esa-helper version` said.
func (s *Service) recordHelper(ctx context.Context, serverID int64, v proto.VersionInfo) {
	caps := store.JSON("null")
	if v.Capabilities != nil {
		caps = store.MustJSON(v.Capabilities)
	}
	now := s.now()
	err := s.db.WithContext(context.WithoutCancel(ctx)).Model(&store.Server{}).Where("id = ?", serverID).Updates(map[string]any{
		"helper_version": v.Version, "arch": v.Arch, "helper_protocol": v.Protocol,
		"helper_capabilities": caps, "helper_checked_at": now,
	}).Error
	if err != nil {
		s.log.Error("record helper version", "server_id", serverID, "err", err)
	}
}

// runActions are the helper actions a run of this kind uses.
func runActions(kind RunKind) []string {
	switch kind {
	case RunMeasure, RunClean:
		return []string{"clean"}
	case RunUpdate:
		return []string{"status", "upgrade-precheck", "upgrade-start", "upgrade-status"}
	case RunReboot:
		return []string{"status", "containers", "reboot"}
	}
	return nil
}

// helperGaps names the servers whose helper lacks an action the run needs,
// as "name (action, action)". Servers whose helper was never asked are
// judged by the legacy set, which has every run action.
func helperGaps(actions []string, servers []store.Server) []string {
	var out []string
	for _, srv := range servers {
		caps := storedCapabilities(srv)
		var missing []string
		for _, a := range actions {
			if !proto.Supports(caps, a) {
				missing = append(missing, a)
			}
		}
		if len(missing) > 0 {
			out = append(out, srv.Name+" ("+joinComma(missing)+")")
		}
	}
	slices.Sort(out)
	return out
}

func joinComma(items []string) string {
	out := ""
	for i, it := range items {
		if i > 0 {
			out += ", "
		}
		out += it
	}
	return out
}
