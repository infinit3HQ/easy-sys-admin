//go:build !linux

package hostops

import "errors"

// The helper only runs on Linux. This stub lets the package build on
// developer machines for tests and vet.
func statfs(string) (total, used, avail, inodes, inodesUsed uint64, err error) {
	err = errors.New("statfs is only implemented on linux")
	return
}
