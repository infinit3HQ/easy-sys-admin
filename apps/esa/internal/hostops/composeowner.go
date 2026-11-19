package hostops

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Compose runs as root, and the labels it is pointed by can come from an
// image. So the files it reads, and every directory above them, must be ones
// that only users who can already act as root are able to change.
//
// Those users are root and the members of the groups that grant root:
// docker, sudo, wheel and admin. A group is trusted when it is one of those,
// when it is group 0, or when every member of it is trusted. Users and groups
// are read from /etc/passwd and /etc/group, so accounts that only a directory
// service provides are not seen and are refused.

// composeRootGroups are the groups whose members can act as root.
var composeRootGroups = []string{"docker", "sudo", "wheel", "admin"}

type composeTrust struct {
	uid func(uint32) bool
	gid func(uint32) bool
}

func (h *Host) composeTrust() composeTrust {
	t := composeTrust{uid: h.TrustedComposeOwner, gid: h.TrustedComposeGroup}
	if t.uid != nil && t.gid != nil {
		return t
	}
	uids, gids := h.trustedIDs()
	if t.uid == nil {
		t.uid = func(uid uint32) bool { return uid == 0 || uids[uid] }
	}
	if t.gid == nil {
		t.gid = func(gid uint32) bool { return gid == 0 || gids[gid] }
	}
	return t
}

// trustedIDs returns the user and group ids that can act as root.
func (h *Host) trustedIDs() (uids, gids map[uint32]bool) {
	uids, gids = map[uint32]bool{}, map[uint32]bool{}
	groupRaw, err := h.readFile("/etc/group")
	if err != nil {
		return
	}
	passwdRaw, err := h.readFile("/etc/passwd")
	if err != nil {
		return
	}
	type group struct {
		gid     uint32
		members []string
	}
	var groups []group
	rootNames := map[string]bool{}
	for _, line := range strings.Split(string(groupRaw), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 {
			continue
		}
		gid, err := strconv.ParseUint(f[2], 10, 32)
		if err != nil {
			continue
		}
		g := group{gid: uint32(gid)}
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				g.members = append(g.members, m)
			}
		}
		groups = append(groups, g)
		for _, name := range composeRootGroups {
			if f[0] == name {
				gids[g.gid] = true
				for _, m := range g.members {
					rootNames[m] = true
				}
			}
		}
	}
	type user struct {
		name     string
		uid, gid uint32
	}
	var users []user
	for _, line := range strings.Split(string(passwdRaw), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 {
			continue
		}
		uid, e1 := strconv.ParseUint(f[2], 10, 32)
		gid, e2 := strconv.ParseUint(f[3], 10, 32)
		if e1 != nil || e2 != nil {
			continue
		}
		users = append(users, user{f[0], uint32(uid), uint32(gid)})
	}
	for _, u := range users {
		if rootNames[u.name] || gids[u.gid] {
			uids[u.uid] = true
		}
	}
	trustedName := map[string]bool{"root": true}
	for _, u := range users {
		if uids[u.uid] {
			trustedName[u.name] = true
		}
	}
	// A group is trusted when nobody outside the trusted users is in it,
	// as a listed member or through it being their primary group.
	// A group nobody visible belongs to may have members that only a
	// directory service knows, so it is not trusted.
	for _, g := range groups {
		ok, seen := true, false
		for _, m := range g.members {
			ok, seen = ok && trustedName[m], true
		}
		for _, u := range users {
			if u.gid == g.gid {
				seen = true
				if !trustedName[u.name] && u.uid != 0 {
					ok = false
				}
			}
		}
		if ok && seen {
			gids[g.gid] = true
		}
	}
	return
}

// checkComposeEntry refuses an entry that someone who cannot act as root
// could change. For a directory above the project, world write is fine when
// the sticky bit is set and a trusted user owns it: there, nobody else can
// rename or remove an entry that is not theirs, as in /tmp.
func (h *Host) checkComposeEntry(t composeTrust, path string, fi os.FileInfo, ancestor bool) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return &RefusedError{Reason: path + " has no owner information"}
	}
	perm := fi.Mode().Perm()
	sticky := fi.Mode()&os.ModeSticky != 0
	if perm&0o002 != 0 && !(ancestor && sticky) {
		return &RefusedError{Reason: path + " can be written by anyone; esa will not run compose from it as root"}
	}
	if perm&0o020 != 0 && !t.gid(st.Gid) {
		return &RefusedError{Reason: path + " can be written by a group with members who cannot act as root; esa will not run compose from it"}
	}
	if !t.uid(st.Uid) {
		return &RefusedError{Reason: path + " is owned by a user who cannot act as root (root, docker, sudo, wheel and admin members can); esa will not run compose from it"}
	}
	return nil
}

// checkComposeTree checks the project directory, each compose file, a .env
// file beside them, every directory above the project, and the directories
// between the project and each file. The project directory is resolved to
// its real path first, so a symlink cannot hide the directories that really
// hold it. It is run before compose pulls and again before it recreates the
// service, because a pull can take minutes and paths are checked by name.
func (h *Host) checkComposeTree(dir string, files []string) error {
	t := h.composeTrust()
	real, err := h.realPath(dir)
	if err != nil {
		return &RefusedError{Reason: "compose directory " + dir + " cannot be resolved"}
	}
	for p := real; ; p = filepath.Dir(p) {
		fi, err := os.Lstat(h.path(p))
		if err != nil || !fi.IsDir() {
			return &RefusedError{Reason: "compose directory " + p + " is not a directory"}
		}
		if err := h.checkComposeEntry(t, p, fi, p != real); err != nil {
			return err
		}
		if p == "/" || p == filepath.Dir(p) {
			break
		}
	}
	extra := append([]string{}, files...)
	if env := filepath.Join(dir, ".env"); fileExists(h.path(env)) {
		extra = append(extra, env)
	}
	for _, f := range extra {
		rel, err := filepath.Rel(dir, f)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return &RefusedError{Reason: "compose file " + f + " is outside " + dir}
		}
		// Each directory between the project and the file, then the file.
		p := real
		parts := strings.Split(rel, string(filepath.Separator))
		for i, part := range parts {
			p = filepath.Join(p, part)
			fi, err := os.Lstat(h.path(p))
			last := i == len(parts)-1
			switch {
			case err != nil:
				return &RefusedError{Reason: "compose file " + f + " no longer exists"}
			case last && !fi.Mode().IsRegular():
				return &RefusedError{Reason: "compose file " + f + " is not a regular file"}
			case !last && !fi.IsDir():
				return &RefusedError{Reason: "compose directory " + filepath.Dir(f) + " is not a directory"}
			}
			if err := h.checkComposeEntry(t, p, fi, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// realPath resolves symlinks in p, inside the host root.
func (h *Host) realPath(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(h.path(p))
	if err != nil {
		return "", err
	}
	if h.Root != "" {
		root, err := filepath.EvalSymlinks(h.Root)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", os.ErrNotExist
		}
		return filepath.Join("/", rel), nil
	}
	return resolved, nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
