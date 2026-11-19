package hostops

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRPMInventoryKeepsEpochArchitectureAndSource(t *testing.T) {
	rows, truncated := parseRPMInventory("NetworkManager\t1:1.48.10-6.el9_8\taarch64\tinstalled\tNetworkManager-1.48.10-6.el9_8.src.rpm\ncurl\t0:7.76.1-35.el9\taarch64\tinstalled\tcurl-7.76.1-35.el9.src.rpm\n")
	if truncated || len(rows) != 2 || rows[0].Version != "1:1.48.10-6.el9_8" || rows[0].Source != "NetworkManager" || rows[1].Version != "7.76.1-35.el9" {
		t.Fatalf("inventory: %+v", rows)
	}
}

func TestRPMMissingSecurityMetadataIsNotClean(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("ID=rocky\nVERSION_ID=9.7\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{out: map[string]string{"rpm -qa": "curl\t0:7.76.1-35.el9\taarch64\tinstalled\tcurl-7.76.1-35.el9.src.rpm\n"}}
	h := &Host{Root: root, Run: runner}
	if _, err := h.PendingUpdates(context.Background(), false); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("missing advisory metadata appeared clean: %v", err)
	}
}

func TestRPMTransactionChecksBeforeCommitting(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is needed to exercise the fixed DNF transaction program")
	}
	root := t.TempDir()
	module := `import json,os
class P:
 def __init__(self,s): self.name,self.arch=s.rsplit('.',1)
class Vars:
 def update_from_etc(self,root,paths): pass
class Conf:
 installonlypkgs=[]
 installroot="/"
 varsdir=[]
 substitutions=Vars()
 def read(self): pass
class Repos:
 def iter_enabled(self): return []
class Query:
 def installed(self): return [P('openssl.aarch64')]
class Sack:
 def query(self): return Query()
class Transaction:
 def __init__(self):
  self.install_set=[P(s) for s in json.loads(os.environ['TEST_INSTALL'])]
  self.remove_set=[P(s) for s in json.loads(os.environ['TEST_REMOVE'])]
class Base:
 def __init__(self): self.conf=Conf();self.repos=Repos();self.sack=Sack();self.transaction=Transaction()
 def __enter__(self): return self
 def __exit__(self,*args): pass
 def read_all_repos(self): pass
 def fill_sack(self): pass
 def upgrade(self,s): pass
 def resolve(self,allow_erasing=False):
  if allow_erasing: raise RuntimeError('erasing enabled')
 def download_packages(self,p): print('DOWNLOADED')
 def package_signature_check(self,p): return (int(os.environ['TEST_SIGNATURE']), '')
 def do_transaction(self): print('COMMITTED')
`
	if err := os.WriteFile(filepath.Join(root, "dnf.py"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		install, remove []string
		signature       string
		commit          bool
	}{
		{"approved replacement", []string{"openssl.aarch64"}, []string{"openssl.aarch64"}, "0", true},
		{"unapproved dependency", []string{"openssl.aarch64", "dependency.aarch64"}, nil, "0", false},
		{"unapproved removal", []string{"openssl.aarch64"}, []string{"other.aarch64"}, "0", false},
		{"untrusted signature", []string{"openssl.aarch64"}, nil, "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			install, _ := json.Marshal(tc.install)
			remove, _ := json.Marshal(tc.remove)
			if tc.remove == nil {
				remove = []byte("[]")
			}
			cmd := exec.Command(python, rpmUpgradeArgs([]string{"openssl.aarch64"})...)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "PYTHONPATH=" + root, "TEST_INSTALL=" + string(install), "TEST_REMOVE=" + string(remove), "TEST_SIGNATURE=" + tc.signature}
			out, err := cmd.CombinedOutput()
			committed := strings.Contains(string(out), "COMMITTED")
			if committed != tc.commit || (err == nil) != tc.commit {
				t.Fatalf("commit=%v error=%v output=%s", committed, err, out)
			}
		})
	}
}
