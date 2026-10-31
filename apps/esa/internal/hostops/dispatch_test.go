package hostops

import (
	"reflect"
	"testing"
)

func TestSplitShellWords(t *testing.T) {
	ok := map[string][]string{
		"status":                {"status"},
		"a b":                   {"a", "b"},
		"'a b' c":               {"a b", "c"},
		`'it'\''s'`:             {"it's"},
		`'a'\'''\''b'`:          {"a''b"},
		"''":                    {""},
		"'$(reboot)' x":         {"$(reboot)", "x"},
		"'x;rm -rf /'":          {"x;rm -rf /"},
		"--run-id 12":           {"--run-id", "12"},
		"'line\nbreak'":         {"line\nbreak"},
		"a,b=c:d@e%f+g/h.i_j-k": {"a,b=c:d@e%f+g/h.i_j-k"},
	}
	for in, want := range ok {
		got, err := SplitShellWords(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("SplitShellWords(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", " a", "a ", "a  b", "a\tb", "a;b", "a|b", "a&b", "$(id)", "`id`", "a$b", `"a"`, `a\ b`,
		"'unterminated", "'a'b", "a'b'", `'a'\'`, `'a'\'x'`, "a\nb", "*", "a>b", "~", "{a,b}",
	}
	for _, in := range bad {
		if got, err := SplitShellWords(in); err == nil {
			t.Errorf("SplitShellWords(%q) = %q, want error", in, got)
		}
	}
}

func TestDispatchArgs(t *testing.T) {
	got, err := DispatchArgs("sudo -n /usr/local/lib/esa/esa-helper logs --unit nginx.service --grep 'a b'")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "/usr/local/lib/esa/esa-helper", "logs", "--unit", "nginx.service", "--grep", "a b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, cmd := range []string{
		"",
		"sudo -n /usr/local/lib/esa/esa-helper",
		"sudo -n /usr/local/lib/esa/esa-helper ssh-dispatch",
		"sudo -n /usr/local/lib/esa/esa-helper 'ssh-dispatch'",
		"sudo /usr/local/lib/esa/esa-helper status",
		"sudo -n -u root /usr/local/lib/esa/esa-helper status",
		"sudo -n /bin/sh -c id",
		"sudo -n /usr/local/lib/esa/../esa/esa-helper status",
		"/usr/local/lib/esa/esa-helper status",
		"sh -c 'sudo -n /usr/local/lib/esa/esa-helper status'",
		"sudo -n /usr/local/lib/esa/esa-helper status; id",
		"sudo -n /usr/local/lib/esa/esa-helper status && id",
		"scp -t /tmp",
	} {
		if args, err := DispatchArgs(cmd); err == nil {
			t.Errorf("DispatchArgs(%q) = %q, want error", cmd, args)
		}
	}
}
