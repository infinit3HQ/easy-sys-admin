---
title: Remove a server
description: Delete a server from the board and uninstall the esa user, sudo rule, and helper.
---

Open the server, then its settings, and delete it. "Also remove esa from the server" is on by default. The API is `DELETE /api/v1/servers/{serverId}?uninstall=true`.

The control plane calls `esa-helper uninstall` and waits up to 30 seconds. If that call fails, the delete is refused, so you can fix the connection or delete without removing the helper.

Uninstall starts a short-lived systemd unit that runs about 5 seconds later. The `esa` user's own SSH session would otherwise block `userdel`. The unit runs:

```sh
rm -f /etc/sudoers.d/esa /etc/sudoers.d/esa.new
if [ "$(getent passwd esa | cut -d: -f5,6)" = "easy-sys-admin control plane:/var/lib/esa-home" ]; then
  pkill -u esa || true
  sleep 1
  userdel --remove esa 2>/dev/null || userdel esa || true
  rm -rf /var/lib/esa-home
fi
rm -rf /var/lib/esa /usr/local/lib/esa
```

The user and its home are removed only when the comment and home match the ones Easy Sys Admin created. A user named `esa` that you created for something else is left in place. The sudo file and `/usr/local/lib/esa` are still removed.

Deleting a server with uninstall switched off, or deleting an organization, leaves the user, the key, and the sudo rule on the machine. To clean up by hand, run the script above as root.

After a successful uninstall, the control plane has no SSH login on that server. Adding it again is a new enrollment.
