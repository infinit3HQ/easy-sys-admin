package hostops

import (
	"context"
	"strconv"
	"time"
)

// uninstallScript removes everything InstallScript created. It runs as a
// transient systemd unit a few seconds later, because the esa user's own
// SSH session would otherwise block `userdel`.
const uninstallScript = `rm -f /etc/sudoers.d/esa /etc/sudoers.d/esa.new
pkill -u esa || true
sleep 1
userdel --remove esa 2>/dev/null || userdel esa || true
rm -rf /var/lib/esa-home /var/lib/esa /usr/local/lib/esa`

// Uninstall schedules removal of the esa user, its sudo rule, and the helper.
func (h *Host) Uninstall(ctx context.Context) error {
	_, err := h.output(ctx, 30*time.Second, "systemd-run",
		"--unit=esa-uninstall-"+strconv.FormatInt(time.Now().Unix(), 10),
		"--on-active=5", "--timer-property=AccuracySec=1s",
		"/bin/sh", "-c", uninstallScript)
	return err
}
