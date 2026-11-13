package fleet

import (
	"context"
	"time"
)

// EvidenceServer is one visible server's stored update and exposure ages.
// It is not a live access report and has no user list.
type EvidenceServer struct {
	ID                int64
	Name              string
	UpdatesCheckedAt  *time.Time
	SecurityUpdates   int
	OtherUpdates      int
	UpdatesStale      bool
	ExposureCheckedAt *time.Time
	ExposureReady     bool
	ExposureStale     bool
}

// EvidenceKey is a fingerprint seen on visible servers. It is not key material.
type EvidenceKey struct {
	Fingerprint string
	Label       string
	Servers     []string
}

// EvidenceExport is the download of evidence the actor can already see.
type EvidenceExport struct {
	ExportedAt time.Time
	Servers    []EvidenceServer
	Keys       []EvidenceKey
}

// Evidence returns update ages, exposure ages, and key fingerprints for
// servers the actor can see. Hidden servers are absent. It does not read
// the servers again.
func (s *Service) Evidence(ctx context.Context, a Actor) (EvidenceExport, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return EvidenceExport{}, err
	}
	ov, err := s.Overview(ctx, a)
	if err != nil {
		return EvidenceExport{}, err
	}
	keys, err := s.ListKeys(ctx, a)
	if err != nil {
		return EvidenceExport{}, err
	}
	now := s.now()
	out := EvidenceExport{ExportedAt: now, Servers: []EvidenceServer{}, Keys: []EvidenceKey{}}
	for _, v := range ov.Servers {
		row := EvidenceServer{ID: v.ID, Name: v.Name, UpdatesCheckedAt: v.UpdatesAt, UpdatesStale: UpdatesAreStale(v.UpdatesAt, now)}
		if v.Updates != nil {
			row.SecurityUpdates = v.Updates.Security
			row.OtherUpdates = v.Updates.Other
		}
		if v.Exposure != nil {
			row.ExposureCheckedAt = &v.Exposure.CheckedAt
			row.ExposureReady = v.Exposure.Ready
			row.ExposureStale = v.Exposure.Stale(now)
		} else {
			row.ExposureStale = true
		}
		out.Servers = append(out.Servers, row)
	}
	for _, k := range keys {
		seen := map[string]bool{}
		var names []string
		for _, u := range k.Uses {
			if seen[u.ServerName] {
				continue
			}
			seen[u.ServerName] = true
			names = append(names, u.ServerName)
		}
		if names == nil {
			names = []string{}
		}
		out.Keys = append(out.Keys, EvidenceKey{Fingerprint: k.Fingerprint, Label: k.Label, Servers: names})
	}
	return out, nil
}
