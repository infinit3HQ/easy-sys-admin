package fleet

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Canary gate states.
const (
	GateWaiting   = "waiting"
	GateContinued = "continued"
	GateStopped   = "stopped"
	GateExpired   = "expired"
)

// gateTimeout is how long a run waits at its canary gate. An unanswered
// gate stops the run: silence is never consent to go on.
const gateTimeout = 24 * time.Hour

// gatePollEvery is how often a waiting run looks for a decision. Tests
// shorten it.
var gatePollEvery = 5 * time.Second

// GateView is a run's canary gate as the front doors show it.
type GateView struct {
	State string
	At    *time.Time
	By    string
	Note  string
}

// awaitGate pauses a run after its canaries until a person decides. It
// returns true to go on and, when false, why the rest stops. The state is
// stored, so after a restart a waiting run waits again and a decided one
// keeps its decision. When ctx ends (the process stops) it returns false
// without recording anything.
func (s *Service) awaitGate(ctx context.Context, runID int64) (bool, string) {
	db := s.db.WithContext(context.WithoutCancel(ctx))
	var run store.Run
	if err := db.Select("id", "account_id", "gate_state", "gate_at").Where("id = ?", runID).Take(&run).Error; err != nil {
		return false, "stopped: the run could not be read at the canary gate"
	}
	switch run.GateState {
	case GateContinued:
		return true, ""
	case GateStopped:
		return false, "stopped by a person at the canary gate"
	case GateExpired:
		return false, "stopped: nobody answered the canary gate within " + gateTimeout.String()
	}
	if run.GateState == "" {
		now := s.now()
		db.Model(&store.Run{}).Where("id = ? AND gate_state = ''", runID).Updates(map[string]any{"gate_state": GateWaiting, "gate_at": now})
		run.GateAt = &now
		s.notify(ctx, run.AccountID, Message{
			Title:    "Run " + strconv.FormatInt(runID, 10) + " is waiting at the canary gate",
			Body:     "The canaries passed. Continue or stop the run on its page.",
			Severity: "warning", Kind: kindRunGate,
			URL: s.publicURL.JoinPath("/app/runs", strconv.FormatInt(runID, 10)).String(),
		})
	}
	deadline := run.GateAt.Add(gateTimeout)
	for {
		if ctx.Err() != nil {
			return false, ""
		}
		var cur store.Run
		if err := db.Select("gate_state").Where("id = ?", runID).Take(&cur).Error; err == nil {
			switch cur.GateState {
			case GateContinued:
				return true, ""
			case GateStopped:
				return false, "stopped by a person at the canary gate"
			}
		}
		if s.now().After(deadline) {
			db.Model(&store.Run{}).Where("id = ? AND gate_state = ?", runID, GateWaiting).Update("gate_state", GateExpired)
			return false, "stopped: nobody answered the canary gate within " + gateTimeout.String()
		}
		select {
		case <-ctx.Done():
			return false, ""
		case <-time.After(gatePollEvery):
		}
	}
}

// kindRunGate marks the gate notice, which is never held for a summary.
const kindRunGate = "run_gate"

// DecideGate continues or stops a run waiting at its canary gate. Only a
// person signed in to the web app, with the admin role in the space that
// owns the run, can decide: a token cannot let an update spread.
func (s *Service) DecideGate(ctx context.Context, a Actor, runID int64, decision, note string) (RunView, error) {
	if !a.isPerson() {
		return RunView{}, ErrForbidden
	}
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return RunView{}, err
	}
	if decision != "continue" && decision != "stop" {
		return RunView{}, invalid("decision", "must be continue or stop")
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 200 {
		return RunView{}, invalid("note", "must be at most 200 characters")
	}
	var run store.Run
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", runID, a.AccountID).Take(&run).Error; err != nil {
		return RunView{}, mapStore(err)
	}
	state := GateContinued
	if decision == "stop" {
		state = GateStopped
	}
	res := s.db.WithContext(ctx).Model(&store.Run{}).Where("id = ? AND gate_state = ?", runID, GateWaiting).
		Updates(map[string]any{"gate_state": state, "gate_by": a.UserID, "gate_note": note})
	if res.Error != nil {
		return RunView{}, res.Error
	}
	if res.RowsAffected == 0 {
		return RunView{}, invalid("decision", "the run is not waiting at its canary gate")
	}
	s.audit(ctx, nil, a, "run.gate", strconv.FormatInt(runID, 10), map[string]any{"decision": decision, "note": note})
	return s.GetRun(ctx, a, runID)
}
