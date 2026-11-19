package server

import "testing"

func TestBodyLimit(t *testing.T) {
	if bodyLimit("/api/v1/servers/12/scan") != maxScanBytes {
		t.Fatal("the scan import needs the larger cap")
	}
	for _, p := range []string{"/api/v1/servers/12/scan/x", "/api/v1/servers/abc/scan", "/api/v1/plans", "/auth/login"} {
		if bodyLimit(p) != maxBodyBytes {
			t.Fatalf("%s must keep 1 MiB", p)
		}
	}
}
