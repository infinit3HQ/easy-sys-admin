package hostops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// Locks live in the root-owned state directory. Keep their inodes after
// unlock: unlinking a lock file could let another caller lock a new inode.
func (h *Host) upgradeLock(name string) (func(), error) {
	if err := os.MkdirAll(h.path(proto.StateDir), 0750); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(h.path(filepath.Join(proto.StateDir, name)), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, err
		}
		return nil, &BusyError{Reason: "another helper is starting or executing this upgrade"}
	}
	return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
}

func upgradeStartLockName(id int64) string { return fmt.Sprintf("upgrade-%d.start.lock", id) }
