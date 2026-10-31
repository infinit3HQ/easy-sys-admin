package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// MountEnroll registers the public enroll endpoints used by the enroll
// script. They are authenticated by the one-time token in the path.
//
//	GET  /enroll/{token}           the shell script
//	GET  /enroll/{token}/helper    the helper binary (?arch=amd64|arm64)
//	POST /enroll/{token}/complete  the server reports its host key
func MountEnroll(mux *http.ServeMux, fl *fleet.Service, log *slog.Logger) {
	fail := func(w http.ResponseWriter, err error) {
		status := http.StatusInternalServerError
		var verr *fleet.ValidationError
		switch {
		case errors.Is(err, fleet.ErrEnrollTokenInvalid):
			status = http.StatusGone
		case errors.As(err, &verr):
			status = http.StatusBadRequest
		default:
			log.Error("enroll", "err", err)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		// The script runs `curl -f`, so this text only shows up for humans.
		_, _ = io.WriteString(w, "echo 'esa: "+safeText(err.Error())+"' >&2; exit 1\n")
	}
	mux.HandleFunc("GET /enroll/{token}", func(w http.ResponseWriter, r *http.Request) {
		script, err := fl.EnrollScript(r.Context(), r.PathValue("token"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, script)
	})
	mux.HandleFunc("GET /enroll/{token}/helper", func(w http.ResponseWriter, r *http.Request) {
		bin, err := fl.EnrollHelper(r.Context(), r.PathValue("token"), r.URL.Query().Get("arch"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(bin)
	})
	mux.HandleFunc("POST /enroll/{token}/complete", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
		if err != nil {
			fail(w, err)
			return
		}
		if err := fl.CompleteEnrollment(r.Context(), r.PathValue("token"), string(body)); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// safeText keeps an error message safe inside single quotes in a shell script.
func safeText(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\'' || r == '\\' || r < 0x20 {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}
