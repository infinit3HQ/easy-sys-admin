package hostops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

var containerRefRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]{0,127}$`)

// DefaultProtectedContainers are matched against container names by prefix.
// They run the public edge (proxy, auth, firewall, management), so every
// action on them needs the name typed as confirmation.
var DefaultProtectedContainers = []string{"traefik", "authentik", "portainer", "crowdsec"}

// ContainerFilter carries the protected list for this server.
type ContainerFilter struct {
	Protected []string // name prefixes; nil means DefaultProtectedContainers
}

func (f ContainerFilter) isProtected(name string) bool {
	list := f.Protected
	if list == nil {
		list = DefaultProtectedContainers
	}
	name = strings.ToLower(name)
	for _, p := range list {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" && strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Containers lists every container with health and restart details.
func (h *Host) Containers(ctx context.Context, f ContainerFilter) ([]proto.Container, error) {
	if h.Docker == nil {
		return []proto.Container{}, nil
	}
	list, err := h.Docker.containers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]proto.Container, 0, len(list))
	for _, c := range list {
		name := c.ID[:12]
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		pc := proto.Container{
			ID: c.ID[:12], Name: name, Image: c.Image, ImageID: c.ImageID, State: c.State, Status: c.Status,
			CreatedAt:      time.Unix(c.Created, 0).UTC(),
			ComposeProject: c.Labels["com.docker.compose.project"],
			ComposeService: c.Labels["com.docker.compose.service"],
			Protected:      f.isProtected(name),
			Ports:          []string{},
		}
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				pc.Ports = append(pc.Ports, fmt.Sprintf("%s:%d->%d/%s", p.IP, p.PublicPort, p.PrivatePort, p.Type))
			}
		}
		if ins, err := h.Docker.inspect(ctx, c.ID); err == nil {
			pc.RestartCount = ins.RestartCount
			pc.RestartPolicy = ins.HostConfig.RestartPolicy.Name
			if ins.State.Health != nil {
				pc.Health = ins.State.Health.Status
			}
			if t, err := time.Parse(time.RFC3339Nano, ins.State.StartedAt); err == nil && t.Year() > 1 {
				pc.StartedAt = t.UTC()
			}
			if ins.Config.Image != "" {
				pc.Image = ins.Config.Image
			}
		}
		out = append(out, pc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// findContainer resolves a name or short id to one container.
func (h *Host) findContainer(ctx context.Context, ref string, f ContainerFilter) (proto.Container, error) {
	if !containerRefRe.MatchString(ref) {
		return proto.Container{}, &BadRequestError{Reason: "invalid container name or id"}
	}
	cs, err := h.Containers(ctx, f)
	if err != nil {
		return proto.Container{}, err
	}
	for _, c := range cs {
		if c.Name == ref || c.ID == ref {
			return c, nil
		}
	}
	return proto.Container{}, &BadRequestError{Reason: "no container named " + ref}
}

// ContainerAction starts, stops, or restarts one container. Protected
// containers need confirm set to the container's name.
func (h *Host) ContainerAction(ctx context.Context, ref, action, confirm string, f ContainerFilter) error {
	if h.Docker == nil {
		return &BadRequestError{Reason: "Docker is not installed"}
	}
	c, err := h.findContainer(ctx, ref, f)
	if err != nil {
		return err
	}
	if c.Protected && confirm != c.Name {
		return &RefusedError{Reason: c.Name + " is protected; confirm with its name"}
	}
	return h.Docker.containerAction(ctx, c.ID, action)
}

// ImageCheck compares each container's local image digest with the registry.
func (h *Host) ImageCheck(ctx context.Context, f ContainerFilter) ([]proto.ImageCheck, error) {
	cs, err := h.Containers(ctx, f)
	if err != nil {
		return nil, err
	}
	seen := map[string]proto.ImageCheck{}
	out := make([]proto.ImageCheck, 0, len(cs))
	for _, c := range cs {
		chk := proto.ImageCheck{Container: c.Name, Image: c.Image}
		if strings.HasPrefix(c.Image, "sha256:") || strings.Contains(c.Image, "@sha256:") {
			chk.Error = "pinned by digest"
			out = append(out, chk)
			continue
		}
		if prev, ok := seen[c.Image]; ok {
			prev.Container = c.Name
			out = append(out, prev)
			continue
		}
		img, err := h.Docker.imageInspect(ctx, c.ImageID)
		if err != nil || len(img.RepoDigests) == 0 {
			chk.Error = "image was built locally or has no registry digest"
			out = append(out, chk)
			continue
		}
		chk.LocalDigest = digestOf(img.RepoDigests[0])
		remote, err := h.output(ctx, time.Minute, "docker", "buildx", "imagetools", "inspect", "--format", "{{json .Manifest.Digest}}", c.Image)
		if err != nil {
			chk.Error = "registry: " + err.Error()
		} else {
			_ = json.Unmarshal([]byte(remote), &chk.RemoteDigest)
			chk.UpdateAvailble = chk.RemoteDigest != "" && !hasDigest(img.RepoDigests, chk.RemoteDigest)
		}
		seen[c.Image] = chk
		out = append(out, chk)
	}
	return out, nil
}

func digestOf(repoDigest string) string {
	_, d, _ := strings.Cut(repoDigest, "@")
	return d
}

func hasDigest(repoDigests []string, d string) bool {
	for _, rd := range repoDigests {
		if digestOf(rd) == d {
			return true
		}
	}
	return false
}

// ContainerUpdate pulls the image of one compose service and recreates only
// that service. Containers outside compose are refused, because recreating
// them would need their full run options.
func (h *Host) ContainerUpdate(ctx context.Context, ref, confirm string, f ContainerFilter) (string, error) {
	c, err := h.findContainer(ctx, ref, f)
	if err != nil {
		return "", err
	}
	if c.Protected && confirm != c.Name {
		return "", &RefusedError{Reason: c.Name + " is protected; confirm with its name"}
	}
	ins, err := h.Docker.inspect(ctx, c.ID)
	if err != nil {
		return "", err
	}
	labels := ins.Config.Labels
	dir := labels["com.docker.compose.project.working_dir"]
	files := labels["com.docker.compose.project.config_files"]
	if c.ComposeProject == "" || c.ComposeService == "" || dir == "" {
		return "", &RefusedError{Reason: c.Name + " is not managed by docker compose"}
	}
	args := []string{"compose", "--project-directory", dir, "-p", c.ComposeProject}
	for _, file := range strings.Split(files, ",") {
		if file = strings.TrimSpace(file); file == "" {
			continue
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		if _, err := os.Stat(h.path(file)); err != nil {
			return "", &RefusedError{Reason: "compose file " + file + " no longer exists"}
		}
		args = append(args, "-f", file)
	}
	pull, err := h.output(ctx, 10*time.Minute, "docker", append(args, "pull", c.ComposeService)...)
	if err != nil {
		return "", err
	}
	up, err := h.output(ctx, 10*time.Minute, "docker", append(args, "up", "-d", "--no-deps", c.ComposeService)...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(pull + "\n" + up), nil
}
