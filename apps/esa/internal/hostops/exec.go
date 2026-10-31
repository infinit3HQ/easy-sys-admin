// Package hostops is the code that runs on managed servers inside
// esa-helper. Every command it runs is a fixed program with typed arguments;
// nothing goes through a shell, and no argument comes from free text.
package hostops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes programs. Tests replace it to record every command, which
// is how the "rules with no switch" are enforced in tests.
type Runner interface {
	Run(ctx context.Context, timeout time.Duration, name string, args ...string) (Result, error)
}

// Result is a finished command.
type Result struct {
	Stdout []byte
	Stderr []byte
	Exit   int
}

// ExecRunner runs real programs with a C locale, so output is parseable.
type ExecRunner struct{}

// Run implements Runner. A non-zero exit is not an error; check Result.Exit.
func (ExecRunner) Run(ctx context.Context, timeout time.Duration, name string, args ...string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "SYSTEMD_PAGER=", "SYSTEMD_COLORS=0")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.Exit = exitErr.ExitCode()
	case ctx.Err() != nil:
		return res, fmt.Errorf("%s: timed out after %s", name, timeout)
	default:
		return res, fmt.Errorf("%s: %w", name, err)
	}
	return res, nil
}

// Host bundles what the helper needs to inspect and change the server.
type Host struct {
	Run    Runner
	Docker *DockerClient // nil when Docker is not installed
	// Root lets tests point file reads at a fixture tree. Empty means "/".
	Root string
}

// NewHost returns a Host for the real machine.
func NewHost() *Host {
	h := &Host{Run: ExecRunner{}}
	if d := NewDockerClient(DefaultDockerSocket); d.Available() {
		h.Docker = d
	}
	return h
}

func (h *Host) path(p string) string {
	if h.Root == "" {
		return p
	}
	return h.Root + p
}

func (h *Host) readFile(p string) ([]byte, error) { return os.ReadFile(h.path(p)) }

// output runs a command and returns trimmed stdout, failing on non-zero exit.
func (h *Host) output(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	res, err := h.Run.Run(ctx, timeout, name, args...)
	if err != nil {
		return "", err
	}
	if res.Exit != 0 {
		return "", fmt.Errorf("%s %s: exit %d: %s", name, strings.Join(args, " "), res.Exit, firstLine(res.Stderr))
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// lookPath reports whether a program is installed.
func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// RefusedError means a safety rule blocked the request.
type RefusedError struct{ Reason string }

func (e *RefusedError) Error() string { return "refused: " + e.Reason }

// BadRequestError means the arguments are invalid.
type BadRequestError struct{ Reason string }

func (e *BadRequestError) Error() string { return e.Reason }

// BusyError means another package operation is in progress.
type BusyError struct{ Reason string }

func (e *BusyError) Error() string { return e.Reason }

// ValidJournalSize reports whether s is a journal size the helper accepts.
// The control plane uses it to validate group settings up front.
func ValidJournalSize(s string) bool { return validJournalSize(s) }

// ValidPackageName reports whether s is a package name the helper accepts.
func ValidPackageName(s string) bool { return pkgNameRe.MatchString(s) }
