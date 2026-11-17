package server

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

//go:embed outbound-install.sh
var outboundInstall []byte

// mountAgentDownloads publishes only this build's fixed bootstrap artifacts.
// Downloads contain no account data or credentials.
func mountAgentDownloads(mux *http.ServeMux, dir string) {
	mux.HandleFunc("GET /enroll/outbound/install.sh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(outboundInstall)
	})
	mux.HandleFunc("GET /enroll/outbound/binaries/{kind}/{arch}", func(w http.ResponseWriter, r *http.Request) {
		kind, arch := r.PathValue("kind"), r.PathValue("arch")
		if (kind != "agent" && kind != "helper") || (arch != "amd64" && arch != "arm64") {
			http.NotFound(w, r)
			return
		}
		raw, err := os.ReadFile(filepath.Join(dir, "esa-"+kind+"-linux-"+arch))
		if err != nil {
			http.Error(w, "Build artifacts are unavailable.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-SHA256", fmt.Sprintf("%x", sha256.Sum256(raw)))
		_, _ = w.Write(raw)
	})
}
