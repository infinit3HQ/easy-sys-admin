package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

func key(b byte) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(b), 32)))
}

func TestSealOpen(t *testing.T) {
	box, err := NewBox(key('a'))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("hunter2"), "server_login:1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "hunter2") {
		t.Fatal("plaintext visible in ciphertext")
	}
	pt, err := box.Open(sealed, "server_login:1")
	if err != nil || string(pt) != "hunter2" {
		t.Fatalf("open = %q, %v", pt, err)
	}
	if _, err := box.Open(sealed, "server_login:2"); err == nil {
		t.Fatal("a ciphertext copied to another row must not open")
	}
}

func TestRotation(t *testing.T) {
	old, _ := NewBox(key('a'))
	sealed, _ := old.Seal([]byte("x"), "c")
	rotated, err := NewBox(key('b'), key('a'))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.Open(sealed, "c"); err != nil {
		t.Fatalf("old secrets must open after rotation: %v", err)
	}
	fresh, _ := rotated.Seal([]byte("y"), "c")
	if _, err := old.Open(fresh, "c"); err == nil {
		t.Fatal("new secrets use the new key")
	}
}

func TestBadKeys(t *testing.T) {
	for _, k := range []string{"", "short", base64.StdEncoding.EncodeToString([]byte("16 bytes only!!!"))} {
		if _, err := NewBox(k); err == nil {
			t.Errorf("key %q accepted", k)
		}
	}
}
