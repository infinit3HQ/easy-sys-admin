package proto

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// An outbound agent runs helper actions that the control plane sends over
// HTTPS. A TLS terminator or proxy in front of the control plane can read and
// rewrite that traffic, so each command is signed with the account's key and
// the agent checks the signature against a pin it received when it was set
// up, out of band, with its credential.

// AgentPinPrefix starts every agent pin.
const AgentPinPrefix = "esa_pin_"

// AgentPin names the server an agent serves and the key that signs its commands.
type AgentPin struct {
	ServerID int64
	Key      ed25519.PublicKey
}

// FormatAgentPin returns the pin text shown once beside the agent credential.
func FormatAgentPin(serverID int64, key ed25519.PublicKey) string {
	return AgentPinPrefix + strconv.FormatInt(serverID, 10) + "_" + base64.RawURLEncoding.EncodeToString(key)
}

// ParseAgentPin reads a pin written by FormatAgentPin.
func ParseAgentPin(s string) (AgentPin, error) {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, AgentPinPrefix)
	if !ok || len(s) > 200 {
		return AgentPin{}, errors.New("invalid agent pin")
	}
	idText, keyText, ok := strings.Cut(rest, "_")
	if !ok {
		return AgentPin{}, errors.New("invalid agent pin")
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		return AgentPin{}, errors.New("invalid agent pin")
	}
	key, err := base64.RawURLEncoding.DecodeString(keyText)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return AgentPin{}, errors.New("invalid agent pin")
	}
	return AgentPin{ServerID: id, Key: ed25519.PublicKey(key)}, nil
}

// AgentCommandMessage is the exact byte string that is signed. Arguments are
// length-prefixed, so no choice of arguments can imitate another command.
func AgentCommandMessage(serverID, id int64, action string, args []string, expiresUnix int64) []byte {
	var b strings.Builder
	b.WriteString("esa-agent-command-v1\n")
	b.WriteString(strconv.FormatInt(serverID, 10))
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(id, 10))
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(len(action)))
	b.WriteByte(':')
	b.WriteString(action)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(expiresUnix, 10))
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(len(args)))
	b.WriteByte('\n')
	for _, a := range args {
		b.WriteString(strconv.Itoa(len(a)))
		b.WriteByte(':')
		b.WriteString(a)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// VerifyAgentCommand reports whether signature (base64) signs the command for
// the pinned server and key.
func (p AgentPin) VerifyAgentCommand(serverID, id int64, action string, args []string, expiresUnix int64, signature string) bool {
	if serverID != p.ServerID {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(p.Key, AgentCommandMessage(serverID, id, action, args, expiresUnix), sig)
}
