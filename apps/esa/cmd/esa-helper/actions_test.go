package main

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// Every action the helper dispatches must be in proto.Actions, and the
// list must not claim an action the helper does not have. The control
// plane trusts this list to decide what it may send.
func TestActionListMatchesDispatch(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, m := range regexp.MustCompile(`case ("[a-z-]+"(?:, "[a-z-]+")*):`).FindAllStringSubmatch(string(src), -1) {
		for _, a := range regexp.MustCompile(`"([a-z-]+)"`).FindAllStringSubmatch(m[1], -1) {
			if !slices.Contains(found, a[1]) {
				found = append(found, a[1])
			}
		}
	}
	found = append(found, "version")
	for _, a := range found {
		if !slices.Contains(proto.Actions, a) {
			t.Errorf("helper dispatches %q but proto.Actions does not list it", a)
		}
	}
	for _, a := range proto.Actions {
		if !slices.Contains(found, a) {
			t.Errorf("proto.Actions lists %q but the helper has no case for it", a)
		}
	}
}
