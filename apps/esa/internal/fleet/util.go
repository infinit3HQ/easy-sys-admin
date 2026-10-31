package fleet

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
)

func sortStable[T any](xs []T, less func(a, b T) bool) {
	slices.SortStableFunc(xs, func(a, b T) int {
		switch {
		case less(a, b):
			return -1
		case less(b, a):
			return 1
		}
		return 0
	})
}

func parseKey(s string) (ssh.PublicKey, error) { return remote.ParseKey(s) }

func fingerprint(k ssh.PublicKey) string { return remote.Fingerprint(k) }

// randomToken returns a URL-safe token and its SHA-256.
func randomToken(prefix string) (token string, hash []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	token = prefix + base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token)
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

var (
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._\-]{0,99}$`)
	hostnameRe = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?)*$`)
)

func validName(field, name string) (string, error) {
	name = strings.TrimSpace(name)
	if !nameRe.MatchString(name) {
		return "", invalid(field, "use 1 to 100 letters, digits, spaces, dots, dashes, or underscores")
	}
	return name, nil
}

func validHost(host string) (string, error) {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if net.ParseIP(host) != nil || (len(host) <= 253 && hostnameRe.MatchString(host)) {
		return host, nil
	}
	return "", invalid("host", "enter an IP address or a hostname")
}

func validPort(port int) (int, error) {
	if port == 0 {
		return 22, nil
	}
	if port < 1 || port > 65535 {
		return 0, invalid("port", "must be between 1 and 65535")
	}
	return port, nil
}

func cleanList(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

var (
	validJournalSize = hostops.ValidJournalSize
	pkgNameRe        = packageNameMatcher{}
)

type packageNameMatcher struct{}

func (packageNameMatcher) MatchString(s string) bool { return hostops.ValidPackageName(s) }
