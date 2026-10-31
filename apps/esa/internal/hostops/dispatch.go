package hostops

import (
	"errors"
	"strings"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// DispatchAction is the helper action the esa key's forced command runs.
// sshd starts it as the esa user with the requested command in
// SSH_ORIGINAL_COMMAND.
const DispatchAction = "ssh-dispatch"

// DispatchArgs checks a command the control plane sent over the esa key and
// returns the arguments for sudo. The only accepted form is the one
// remote.Pool sends: `sudo -n <HelperPath> <action> [args...]`, with every
// word as remote.ShellQuote writes it.
func DispatchArgs(cmd string) ([]string, error) {
	words, err := SplitShellWords(cmd)
	if err != nil {
		return nil, err
	}
	if len(words) < 4 || words[0] != "sudo" || words[1] != "-n" || words[2] != proto.HelperPath {
		return nil, errors.New("only `sudo -n " + proto.HelperPath + " <action>` is allowed")
	}
	if words[3] == DispatchAction {
		return nil, errors.New(DispatchAction + " cannot be called through itself")
	}
	return words[1:], nil
}

// SplitShellWords splits words joined by single spaces, where each word is
// either bare (only characters ShellQuote leaves unquoted) or one
// single-quoted string in which a quote is written as quote, backslash,
// quote, quote. Any other shell syntax is an error.
func SplitShellWords(s string) ([]string, error) {
	if s == "" {
		return nil, errors.New("empty command")
	}
	var words []string
	for i := 0; ; {
		var w string
		var n int
		var ok bool
		if s[i] == '\'' {
			w, n, ok = quotedWord(s[i:])
		} else {
			w, n, ok = bareWord(s[i:])
		}
		if !ok {
			return nil, errors.New("unsupported shell syntax in command")
		}
		words = append(words, w)
		i += n
		if i == len(s) {
			return words, nil
		}
		if s[i] != ' ' || i+1 == len(s) {
			return nil, errors.New("unsupported shell syntax in command")
		}
		i++
	}
}

func isBareChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_./,=:@%+", c) >= 0
}

func bareWord(s string) (string, int, bool) {
	n := 0
	for n < len(s) && isBareChar(s[n]) {
		n++
	}
	return s[:n], n, n > 0
}

// quotedWord reads one quoted word, such as 'abc' or it's quoted by
// ShellQuote, from the start of s.
func quotedWord(s string) (string, int, bool) {
	var b strings.Builder
	i := 0
	for {
		// s[i] is an opening quote.
		end := strings.IndexByte(s[i+1:], '\'')
		if end < 0 {
			return "", 0, false
		}
		b.WriteString(s[i+1 : i+1+end])
		i += end + 2 // past the closing quote
		if !strings.HasPrefix(s[i:], `\''`) {
			return b.String(), i, true
		}
		b.WriteByte('\'')
		i += 2 // at the next opening quote
	}
}
