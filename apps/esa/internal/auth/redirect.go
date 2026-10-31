package auth

import (
	"net/http"
	"net/url"
	"strings"
)

const defaultReturnTo = "/app"

// safeReturnTo accepts only same-site absolute paths, so the login flow
// cannot be used to redirect users to another site.
func safeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return defaultReturnTo
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" {
		return defaultReturnTo
	}
	return u.RequestURI()
}

// loginError sends the browser back to the sign-in page with an error code
// the web app knows how to explain.
func loginError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusSeeOther)
}
