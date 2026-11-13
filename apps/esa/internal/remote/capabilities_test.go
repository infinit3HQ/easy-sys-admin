package remote

import (
	"context"
	"errors"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// An action the helper did not list is refused before any connection. The
// endpoint has no host key, so reaching the network would fail differently.
func TestCallRefusesAnActionTheHelperLacks(t *testing.T) {
	p := NewPool()
	for _, caps := range [][]string{nil, {"status"}} {
		err := p.Call(context.Background(), Endpoint{ServerID: 1, Capabilities: caps}, nil, "inventory")
		var he *HelperError
		if !errors.As(err, &he) || he.Code != proto.CodeIncompatible {
			t.Fatalf("caps %v: got %v, want %s", caps, err, proto.CodeIncompatible)
		}
	}
}
