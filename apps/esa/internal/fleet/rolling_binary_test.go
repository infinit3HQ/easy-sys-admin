package fleet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

var rollingOldBinary = flag.String("rolling-old-binary", "", "prior esa binary for the rolling-upgrade matrix")
var rollingNewBinary = flag.String("rolling-new-binary", "", "current esa binary for the rolling-upgrade matrix")

func TestRollingUpgradeBinaryMatrix(t *testing.T) {
	if *rollingOldBinary == "" || *rollingNewBinary == "" {
		t.Skip("pass -rolling-old-binary and -rolling-new-binary to run the executable matrix")
	}
	s := newTestService(t)
	ctx := context.Background()
	res, err := s.SignIn(ctx, Identity{Issuer: "dev", Subject: "dev", Email: "dev@localhost", Name: "Dev Admin"}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	dev := Actor{UserID: res.User.ID, AccountID: res.AccountID, Role: RoleOwner, Source: SourceWeb}
	org, err := s.CreateOrganization(ctx, dev, "Rolling matrix")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(dev, org)
	bob := signIn(t, s, "rolling-second")
	joinTeam(t, s, owner, bob, RoleAdmin)
	group, err := s.CreateGroup(ctx, owner, GroupInput{Name: "guarded", Timezone: "UTC", Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00", PlanRequired: true, TwoApprovers: true}})
	if err != nil {
		t.Fatal(err)
	}
	srv := addTestServer(t, s, owner, &group.ID, "inert-pending-server")
	if err := s.db.Model(&srv).Update("helper_capabilities", store.MustJSON(proto.Capabilities())).Error; err != nil {
		t.Fatal(err)
	}
	start := func(binary string) (string, *http.Client) {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address := ln.Addr().String()
		_ = ln.Close()
		origin := "http://" + address
		processCtx, stop := context.WithCancel(ctx)
		cmd := exec.CommandContext(processCtx, binary, "serve")
		cmd.Env = append(os.Environ(), "ESA_LISTEN="+address, "ESA_PUBLIC_URL="+origin, "ESA_AUTH_MODE=dev", "ESA_SIGNUP=false", "ESA_SCANNER=", "ESA_AUDIT_RETENTION=", "ESA_REPORT_RETENTION=", "ESA_AUDIT_HOLD_UNTIL=", "ESA_DATABASE_URL="+os.Getenv("ESA_TEST_DATABASE_URL"), "ESA_MASTER_KEY="+base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Start(); err != nil {
			stop()
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(); _ = cmd.Wait() })
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		ready := false
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
			r, err := client.Get(origin + "/healthz")
			if err == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					ready = true
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ready {
			t.Fatal("control plane did not become healthy")
		}
		request := func(method, path string, body any) int {
			var data bytes.Buffer
			if body != nil {
				if err := json.NewEncoder(&data).Encode(body); err != nil {
					t.Fatal(err)
				}
			}
			req, err := http.NewRequest(method, origin+path, &data)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			r, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Body.Close()
			_, _ = io.Copy(io.Discard, r.Body)
			return r.StatusCode
		}
		if status := request("POST", "/auth/dev-login", nil); status != 303 {
			t.Fatalf("dev login: %d", status)
		}
		if status := request("PUT", "/api/v1/me/account", map[string]any{"accountId": owner.AccountID}); status != 200 {
			t.Fatalf("switch: %d", status)
		}
		return origin, client
	}
	current, currentClient := start(*rollingNewBinary)
	prior, priorClient := start(*rollingOldBinary)
	call := func(client *http.Client, origin, method, path string, body any) (int, []byte) {
		t.Helper()
		var b bytes.Buffer
		if body != nil {
			if err := json.NewEncoder(&b).Encode(body); err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequest(method, origin+"/api/v1"+path, &b)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		r, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, data
	}
	for _, which := range []struct {
		name, origin string
		client       *http.Client
	}{{"prior", prior, priorClient}, {"current", current, currentClient}} {
		t.Run(which.name+" proposal", func(t *testing.T) {
			status, data := call(which.client, which.origin, "POST", "/plans", map[string]any{"kind": "measure", "groupId": group.ID})
			if status != 201 {
				t.Fatalf("proposal: %d %s", status, data)
			}
			var proposal struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(data, &proposal); err != nil {
				t.Fatal(err)
			}
			var stored store.FixPlan
			if err := s.db.First(&stored, proposal.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.ApprovalsRequired != 2 {
				t.Fatal("proposal bypassed the group requirement")
			}
			path := fmt.Sprintf("/plans/%d/approve", proposal.ID)
			status, _ = call(priorClient, prior, "POST", path, nil)
			if status >= 200 && status < 300 {
				t.Fatal("prior binary approved with one person")
			}
			status, _ = call(currentClient, current, "POST", path, nil)
			if status != 200 {
				t.Fatalf("current first approval: %d", status)
			}
			if err := s.db.First(&stored, proposal.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != "submitted" || stored.SecondApprovedBy != nil {
				t.Fatal("one person completed approval")
			}
			status, _ = call(priorClient, prior, "POST", path, nil)
			if status >= 200 && status < 300 {
				t.Fatal("prior binary bypassed the remaining approval")
			}
		})
	}
}
