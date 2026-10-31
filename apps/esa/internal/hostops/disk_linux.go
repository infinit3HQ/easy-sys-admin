//go:build linux

package hostops

import "syscall"

func statfs(path string) (total, used, avail, inodes, inodesUsed uint64, err error) {
	var s syscall.Statfs_t
	if err = syscall.Statfs(path, &s); err != nil {
		return
	}
	bs := uint64(s.Bsize)
	total = s.Blocks * bs
	avail = s.Bavail * bs
	used = (s.Blocks - s.Bfree) * bs
	inodes = s.Files
	inodesUsed = s.Files - s.Ffree
	return
}
