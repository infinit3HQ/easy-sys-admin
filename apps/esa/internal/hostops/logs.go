package hostops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// MaxLogLines caps one log request.
const MaxLogLines = 1000

// LogOptions selects log lines. Exactly one of Unit and Container is set.
type LogOptions struct {
	Unit      string
	Container string
	Lines     int
	Priority  int    // 0-7, journal only; -1 means all
	Grep      string // case-insensitive substring
}

func (o *LogOptions) normalize() error {
	if (o.Unit == "") == (o.Container == "") {
		return &BadRequestError{Reason: "give exactly one of --unit or --container"}
	}
	if o.Unit != "" && !unitNameRe.MatchString(o.Unit) {
		return &BadRequestError{Reason: "invalid unit name"}
	}
	if o.Container != "" && !containerRefRe.MatchString(o.Container) {
		return &BadRequestError{Reason: "invalid container name"}
	}
	if o.Lines <= 0 {
		o.Lines = 200
	}
	o.Lines = min(o.Lines, MaxLogLines)
	if o.Priority > 7 {
		o.Priority = 7
	}
	if len(o.Grep) > 200 {
		o.Grep = o.Grep[:200]
	}
	return nil
}

// Logs returns the newest log lines, oldest first.
func (h *Host) Logs(ctx context.Context, o LogOptions) ([]proto.LogLine, error) {
	if err := o.normalize(); err != nil {
		return nil, err
	}
	var lines []proto.LogLine
	var err error
	if o.Unit != "" {
		lines, err = h.journalLines(ctx, o)
	} else {
		lines, err = h.containerLines(ctx, o)
	}
	if err != nil {
		return nil, err
	}
	if o.Grep != "" {
		needle := strings.ToLower(o.Grep)
		kept := lines[:0]
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l.Message), needle) {
				kept = append(kept, l)
			}
		}
		lines = kept
	}
	return lines, nil
}

func (h *Host) journalLines(ctx context.Context, o LogOptions) ([]proto.LogLine, error) {
	args := []string{"-u", o.Unit, "-n", strconv.Itoa(o.Lines), "-o", "json", "--no-pager", "--output-fields=MESSAGE,PRIORITY"}
	if o.Priority >= 0 {
		args = append(args, "-p", strconv.Itoa(o.Priority))
	}
	out, err := h.output(ctx, 30*time.Second, "journalctl", args...)
	if err != nil {
		return nil, err
	}
	lines := []proto.LogLine{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e struct {
			Realtime string          `json:"__REALTIME_TIMESTAMP"`
			Priority string          `json:"PRIORITY"`
			Message  json.RawMessage `json:"MESSAGE"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		us, _ := strconv.ParseInt(e.Realtime, 10, 64)
		prio, err := strconv.Atoi(e.Priority)
		if err != nil {
			prio = 6
		}
		lines = append(lines, proto.LogLine{Time: time.UnixMicro(us).UTC(), Priority: prio, Message: journalMessage(e.Message)})
	}
	return lines, nil
}

// journalMessage decodes MESSAGE, which journald emits as a byte array when
// the text is not valid UTF-8.
func journalMessage(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b []byte
	var ints []int
	if json.Unmarshal(raw, &ints) == nil {
		for _, i := range ints {
			b = append(b, byte(i))
		}
		return strings.ToValidUTF8(string(b), "?")
	}
	return ""
}

func (h *Host) containerLines(ctx context.Context, o LogOptions) ([]proto.LogLine, error) {
	if h.Docker == nil {
		return nil, &BadRequestError{Reason: "Docker is not installed"}
	}
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}, "tail": {strconv.Itoa(o.Lines)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+url.PathEscape(o.Container)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.Docker.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &BadRequestError{Reason: "no container named " + o.Container}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return parseDockerLogs(body, resp.Header.Get("Content-Type")), nil
}

// parseDockerLogs reads the Docker log stream. Without a TTY each frame has
// an 8-byte header: stream type, three zero bytes, big-endian length.
func parseDockerLogs(body []byte, contentType string) []proto.LogLine {
	lines := []proto.LogLine{}
	add := func(stream string, text []byte) {
		for _, l := range strings.Split(strings.TrimRight(string(text), "\n"), "\n") {
			if l == "" {
				continue
			}
			ts, msg, _ := strings.Cut(l, " ")
			t, err := time.Parse(time.RFC3339Nano, ts)
			if err != nil {
				msg = l
			}
			lines = append(lines, proto.LogLine{Time: t.UTC(), Priority: 6, Message: strings.ToValidUTF8(msg, "?"), Stream: stream})
		}
	}
	if strings.Contains(contentType, "raw-stream") && !looksMultiplexed(body) {
		add("stdout", body)
		return lines
	}
	r := bytes.NewReader(body)
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			break
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		frame := make([]byte, size)
		if _, err := io.ReadFull(r, frame); err != nil {
			break
		}
		stream := "stdout"
		if hdr[0] == 2 {
			stream = "stderr"
		}
		add(stream, frame)
	}
	return lines
}

func looksMultiplexed(b []byte) bool {
	return len(b) >= 8 && (b[0] == 1 || b[0] == 2) && b[1] == 0 && b[2] == 0 && b[3] == 0
}
