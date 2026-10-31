package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func cfg(t *testing.T, c fleet.ChannelConfig) []byte {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var msg = fleet.Message{Title: "disk full", Body: "disk / is 95% full", Severity: "critical"}

func TestWebhookAddressPolicy(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusTeapot)
	}))
	defer ts.Close()
	ch := store.NotifyChannel{Kind: "webhook"}
	raw := cfg(t, fleet.ChannelConfig{URL: ts.URL})
	ctx := context.Background()

	if err := New(false).Send(ctx, ch, raw, msg); !netguard.IsBlocked(err) {
		t.Fatalf("loopback webhook with private networks off: %v", err)
	}
	if hits != 0 {
		t.Fatal("the blocked request reached the server")
	}
	err := New(true).Send(ctx, ch, raw, msg)
	var se *fleet.HTTPStatusError
	if !errors.As(err, &se) || se.Code != http.StatusTeapot {
		t.Fatalf("want HTTP 418, got %v", err)
	}
	meta := cfg(t, fleet.ChannelConfig{URL: "http://169.254.169.254/latest/meta-data/"})
	if err := New(true).Send(ctx, ch, meta, msg); !netguard.IsBlocked(err) {
		t.Fatalf("metadata webhook: %v", err)
	}
}

// fakeSMTP accepts one message, or stalls after the greeting when hang is set.
func fakeSMTP(t *testing.T, hang bool) (string, int, chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		if hang {
			time.Sleep(5 * time.Second)
			return
		}
		say("220 test ESMTP")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					got <- data.String()
					say("250 ok")
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-test")
				say("250 8BITMIME")
			case cmd == "DATA":
				inData = true
				say("354 go ahead")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(p)
	return h, port, got
}

func TestEmail(t *testing.T) {
	host, port, got := fakeSMTP(t, false)
	ch := store.NotifyChannel{Kind: "email"}
	raw := cfg(t, fleet.ChannelConfig{SMTPHost: host, SMTPPort: port, From: "esa@example.com", To: []string{"ops@example.com"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := New(false).Send(ctx, ch, raw, msg); !netguard.IsBlocked(err) {
		t.Fatalf("loopback SMTP with private networks off: %v", err)
	}
	if err := New(true).Send(ctx, ch, raw, msg); err != nil {
		t.Fatal(err)
	}
	body := <-got
	if !strings.Contains(body, "Subject: [esa] disk full") || !strings.Contains(body, "disk / is 95% full") {
		t.Fatalf("message = %q", body)
	}
}

// A server that never answers cannot hold a send past its context.
func TestEmailHonoursContext(t *testing.T) {
	host, port, _ := fakeSMTP(t, true)
	raw := cfg(t, fleet.ChannelConfig{SMTPHost: host, SMTPPort: port, From: "esa@example.com", To: []string{"ops@example.com"}})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := New(true).Send(ctx, store.NotifyChannel{Kind: "email"}, raw, msg); err == nil {
		t.Fatal("want a timeout")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("send took %s; the context deadline was 300ms", d)
	}
}
