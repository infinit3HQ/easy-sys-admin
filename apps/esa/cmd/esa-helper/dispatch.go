package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops"
)

// sudoPaths are the only places ssh-dispatch looks for sudo.
var sudoPaths = []string{"/usr/bin/sudo", "/bin/sudo"}

// sshDispatch is the forced command of the esa key. sshd runs it as the esa
// user. It checks SSH_ORIGINAL_COMMAND and replaces itself with sudo, so
// stdin, stdout, stderr, and the exit code pass straight through.
func sshDispatch() int {
	if os.Geteuid() == 0 {
		fmt.Fprintln(os.Stderr, "esa-helper: ssh-dispatch must not run as root")
		return 2
	}
	args, err := hostops.DispatchArgs(os.Getenv("SSH_ORIGINAL_COMMAND"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "esa-helper: refused: "+err.Error())
		return 2
	}
	for _, sudo := range sudoPaths {
		if fi, err := os.Stat(sudo); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		err = syscall.Exec(sudo, append([]string{sudo}, args...), []string{"PATH=" + hostops.SafePath})
		fmt.Fprintln(os.Stderr, "esa-helper: exec sudo: "+err.Error())
		return 1
	}
	fmt.Fprintln(os.Stderr, "esa-helper: sudo not found")
	return 1
}
