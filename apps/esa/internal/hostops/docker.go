package hostops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// DefaultDockerSocket is the Docker Engine API socket.
const DefaultDockerSocket = "/var/run/docker.sock"

// DockerClient talks to the Docker Engine API over its Unix socket. It
// exposes only the calls esa needs; there is deliberately no generic
// "request" method, so no code path can reach system prune, image prune,
// or volume removal.
type DockerClient struct {
	socket string
	http   *http.Client
}

// NewDockerClient returns a client for the socket at path.
func NewDockerClient(path string) *DockerClient {
	return &DockerClient{
		socket: path,
		http: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", path)
				},
			},
		},
	}
}

// Available reports whether the socket exists.
func (d *DockerClient) Available() bool {
	fi, err := os.Stat(d.socket)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

func (d *DockerClient) do(ctx context.Context, method, path string, query url.Values, out any) error {
	u := "http://docker" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = json.Unmarshal(body, &e)
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("docker %s %s: %s: %s", method, path, resp.Status, e.Message)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type dockerVersion struct {
	Version string `json:"Version"`
}

func (d *DockerClient) version(ctx context.Context) (string, error) {
	var v dockerVersion
	err := d.do(ctx, http.MethodGet, "/version", nil, &v)
	return v.Version, err
}

type dockerContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	SizeRw int64 `json:"SizeRw"`
}

func (d *DockerClient) containers(ctx context.Context) ([]dockerContainer, error) {
	var cs []dockerContainer
	err := d.do(ctx, http.MethodGet, "/containers/json", url.Values{"all": {"1"}}, &cs)
	return cs, err
}

type dockerInspect struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	RestartCount int    `json:"RestartCount"`
	Image        string `json:"Image"`
	State        struct {
		Status    string `json:"Status"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
}

func (d *DockerClient) inspect(ctx context.Context, id string) (dockerInspect, error) {
	var c dockerInspect
	err := d.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil, &c)
	return c, err
}

type dockerImageInspect struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
}

func (d *DockerClient) imageInspect(ctx context.Context, ref string) (dockerImageInspect, error) {
	var img dockerImageInspect
	err := d.do(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, &img)
	return img, err
}

type dockerImageList struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
}

// localImages lists images already on this machine. It does not contact a registry.
func (d *DockerClient) localImages(ctx context.Context) ([]proto.LocalImage, error) {
	var raw []dockerImageList
	if err := d.do(ctx, http.MethodGet, "/images/json", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]proto.LocalImage, 0, len(raw))
	for _, img := range raw {
		if len(out) == 500 {
			break
		}
		item := proto.LocalImage{ID: img.ID}
		for _, t := range img.RepoTags {
			if t != "" && t != "<none>:<none>" {
				item.Tags = append(item.Tags, t)
			}
		}
		for _, rd := range img.RepoDigests {
			if dgst := digestOf(rd); dgst != "" {
				item.Digests = append(item.Digests, dgst)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

type dockerDF struct {
	LayersSize int64 `json:"LayersSize"`
	Images     []struct {
		Size       int64 `json:"Size"`
		SharedSize int64 `json:"SharedSize"`
	} `json:"Images"`
	Containers []struct {
		SizeRw int64 `json:"SizeRw"`
	} `json:"Containers"`
	Volumes []struct {
		UsageData struct {
			Size int64 `json:"Size"`
		} `json:"UsageData"`
	} `json:"Volumes"`
	BuildCache []struct {
		Size   int64 `json:"Size"`
		InUse  bool  `json:"InUse"`
		Shared bool  `json:"Shared"`
	} `json:"BuildCache"`
}

func (d *DockerClient) diskUsage(ctx context.Context) (dockerDF, error) {
	var df dockerDF
	err := d.do(ctx, http.MethodGet, "/system/df", nil, &df)
	return df, err
}

// pruneBuildCache removes unused build cache, the same as
// `docker builder prune -f`: only cache that no build references. It never
// sets "all", so cache still referenced by images is kept.
func (d *DockerClient) pruneBuildCache(ctx context.Context) (int64, error) {
	var out struct {
		SpaceReclaimed int64 `json:"SpaceReclaimed"`
	}
	err := d.do(ctx, http.MethodPost, "/build/prune", nil, &out)
	return out.SpaceReclaimed, err
}

// containerAction is start, stop, or restart.
func (d *DockerClient) containerAction(ctx context.Context, id, action string) error {
	switch action {
	case "start", "stop", "restart":
	default:
		return &BadRequestError{Reason: "container action must be start, stop, or restart"}
	}
	q := url.Values{}
	if action != "start" {
		q.Set("t", "30")
	}
	return d.do(ctx, http.MethodPost, "/containers/"+url.PathEscape(id)+"/"+action, q, nil)
}
