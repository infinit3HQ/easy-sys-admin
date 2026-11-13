package fleet

import "context"

// PatchServer is one server's package result and the checks around it.
type PatchServer struct {
	ServerID int64
	Name     string
	Status   string
	Error    string
	Canary   bool
	Packages int
	// Installed, Held, and NotApproved name what the helper did with each
	// pending package. NotApproved are left out because a plan did not
	// list them, often because apt found them after the approval.
	Installed   []string
	Held        []string
	NotApproved []string
	Health      string
	Checks   []CheckResult
}

// PatchReport is what an update run did, including skipped servers.
type PatchReport struct {
	RunID          int64
	Status         string
	Summary        string
	ObserveSeconds int
	Canaries       []int64
	Servers        []PatchServer
}

// PatchReport loads one run the actor can see and shapes the update result.
// A server the actor cannot see is left out.
func (s *Service) PatchReport(ctx context.Context, a Actor, id int64) (PatchReport, error) {
	run, err := s.GetRun(ctx, a, id)
	if err != nil {
		return PatchReport{}, err
	}
	return reportFrom(run), nil
}

func reportFrom(run RunView) PatchReport {
	canary := map[int64]bool{}
	for _, id := range run.Options.CanaryServerIDs {
		canary[id] = true
	}
	out := PatchReport{
		RunID: run.ID, Status: run.Status, Summary: run.Summary,
		ObserveSeconds: run.Options.ObserveSeconds, Canaries: append([]int64(nil), run.Options.CanaryServerIDs...),
		Servers: make([]PatchServer, 0, len(run.Servers)),
	}
	for _, rs := range run.Servers {
		item := PatchServer{
			ServerID: rs.ServerID, Name: rs.ServerName, Status: rs.Status, Error: rs.Error,
			Canary: canary[rs.ServerID], Checks: []CheckResult{},
		}
		var detail struct {
			Health  string        `json:"health"`
			Checks  []CheckResult `json:"checks"`
			Upgrade struct {
				Packages    []string `json:"packages"`
				Skipped     []string `json:"skipped"`
				NotApproved []string `json:"notApproved"`
			} `json:"upgrade"`
		}
		_ = rs.Detail.Decode(&detail)
		item.Health = detail.Health
		item.Packages = len(detail.Upgrade.Packages)
		item.Installed = nonEmpty(detail.Upgrade.Packages)
		item.Held = nonEmpty(detail.Upgrade.Skipped)
		item.NotApproved = nonEmpty(detail.Upgrade.NotApproved)
		if detail.Checks != nil {
			item.Checks = detail.Checks
		}
		out.Servers = append(out.Servers, item)
	}
	return out
}

func nonEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
