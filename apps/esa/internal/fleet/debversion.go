package fleet

import (
	"strconv"
	"strings"
)

// compareDebVersions orders two Debian package versions the way dpkg does:
// epoch, then upstream version, then revision. It returns -1, 0 or 1.
func compareDebVersions(a, b string) int {
	ea, ua, ra := splitDebVersion(a)
	eb, ub, rb := splitDebVersion(b)
	if ea != eb {
		if ea < eb {
			return -1
		}
		return 1
	}
	if c := compareDebPart(ua, ub); c != 0 {
		return c
	}
	return compareDebPart(ra, rb)
}

func splitDebVersion(v string) (epoch int, upstream, revision string) {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ':'); i >= 0 {
		epoch, _ = strconv.Atoi(v[:i])
		v = v[i+1:]
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		return epoch, v[:i], v[i+1:]
	}
	return epoch, v, ""
}

// compareDebPart compares alternating non-digit and digit runs.
func compareDebPart(a, b string) int {
	for a != "" || b != "" {
		var na, nb string
		na, a = leadingNonDigits(a)
		nb, b = leadingNonDigits(b)
		if c := compareDebLexical(na, nb); c != 0 {
			return c
		}
		var da, db string
		da, a = leadingDigits(a)
		db, b = leadingDigits(b)
		if c := compareDebNumber(da, db); c != 0 {
			return c
		}
	}
	return 0
}

func leadingNonDigits(s string) (string, string) {
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		i++
	}
	return s[:i], s[i:]
}

func leadingDigits(s string) (string, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i], s[i:]
}

func compareDebNumber(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// debOrder ranks one character: ~ sorts before everything, even the end of
// the string, and letters sort before other symbols.
func debOrder(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	c := s[i]
	switch {
	case c == '~':
		return -1
	case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		return int(c)
	default:
		return int(c) + 256
	}
}

func compareDebLexical(a, b string) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		oa, ob := debOrder(a, i), debOrder(b, i)
		if oa != ob {
			if oa < ob {
				return -1
			}
			return 1
		}
	}
	return 0
}
