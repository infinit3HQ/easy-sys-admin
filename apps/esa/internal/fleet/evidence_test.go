package fleet

import (
	"context"
	"testing"
)

func TestEvidenceHidesAnotherAccount(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice := signIn(t, s, "evidence-alice")
	bob := signIn(t, s, "evidence-bob")
	addTestServer(t, s, alice, nil, "alice-web")
	addTestServer(t, s, bob, nil, "bob-secret")
	got, err := s.Evidence(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Servers) != 1 || got.Servers[0].Name != "alice-web" {
		t.Fatalf("servers: %+v", got.Servers)
	}
	if !got.Servers[0].UpdatesStale || !got.Servers[0].ExposureStale {
		t.Fatalf("unchecked evidence must be stale: %+v", got.Servers[0])
	}
	viewer := alice
	viewer.Role = RoleViewer
	if _, err := s.Evidence(ctx, viewer); err != nil {
		t.Fatal(err)
	}
	viewer.Scopes = []Scope{}
	if _, err := s.Evidence(ctx, viewer); err == nil {
		t.Fatal("a token without read exported evidence")
	}
}
