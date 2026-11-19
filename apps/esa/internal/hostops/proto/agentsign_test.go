package proto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestAgentPinRoundTrip(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	got, err := ParseAgentPin(FormatAgentPin(42, pub))
	if err != nil || got.ServerID != 42 || !got.Key.Equal(pub) {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for _, bad := range []string{"", "esa_pin_", "esa_pin_x_AAAA", "esa_pin_0_" + base64.RawURLEncoding.EncodeToString(pub),
		"esa_pin_5_short", "token_5_" + base64.RawURLEncoding.EncodeToString(pub)} {
		if _, err := ParseAgentPin(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestAgentCommandSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pin := AgentPin{ServerID: 7, Key: pub}
	args := []string{"--unit", "nginx.service"}
	sign := func(server, id int64, action string, a []string, exp int64) string {
		return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, AgentCommandMessage(server, id, action, a, exp)))
	}
	sig := sign(7, 100, "service", args, 1000)
	if !pin.VerifyAgentCommand(7, 100, "service", args, 1000, sig) {
		t.Fatal("a good signature was refused")
	}
	for name, ok := range map[string]bool{
		"another server": pin.VerifyAgentCommand(8, 100, "service", args, 1000, sig),
		"another id":     pin.VerifyAgentCommand(7, 101, "service", args, 1000, sig),
		"another action": pin.VerifyAgentCommand(7, 100, "reboot", args, 1000, sig),
		"other args":     pin.VerifyAgentCommand(7, 100, "service", []string{"--unit", "ssh.service"}, 1000, sig),
		"fewer args":     pin.VerifyAgentCommand(7, 100, "service", args[:1], 1000, sig),
		"later expiry":   pin.VerifyAgentCommand(7, 100, "service", args, 2000, sig),
		"no signature":   pin.VerifyAgentCommand(7, 100, "service", args, 1000, ""),
		"junk signature": pin.VerifyAgentCommand(7, 100, "service", args, 1000, "AAAA"),
	} {
		if ok {
			t.Errorf("%s was accepted", name)
		}
	}
	// Arguments cannot be re-split to imitate another command.
	if string(AgentCommandMessage(7, 1, "a", []string{"b c"}, 1)) == string(AgentCommandMessage(7, 1, "a", []string{"b", "c"}, 1)) {
		t.Error("two different argument lists encode the same")
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	forged := base64.StdEncoding.EncodeToString(ed25519.Sign(other, AgentCommandMessage(7, 100, "service", args, 1000)))
	if pin.VerifyAgentCommand(7, 100, "service", args, 1000, forged) {
		t.Error("a signature from another key was accepted")
	}
}
