// Package crypto encrypts secrets at rest (SSH keys, passwords, channel
// credentials) with AES-256-GCM and a key that lives only in the control
// plane environment.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// keyIDLen is the length of the key id: the first 8 hex digits of the
// SHA-256 of the key. Stored with each secret so keys can be rotated.
const keyIDLen = 8

// ErrDecrypt means the ciphertext is corrupt, was sealed for another
// context, or was sealed with a key that is not configured.
var ErrDecrypt = errors.New("crypto: cannot decrypt secret")

// Box seals and opens secrets. The zero value is not usable; use NewBox.
type Box struct {
	current string
	aeads   map[string]cipher.AEAD
}

// NewBox builds a Box from base64-encoded 32-byte keys. The first key seals
// new secrets; every key can open existing ones.
func NewBox(keys ...string) (*Box, error) {
	if len(keys) == 0 {
		return nil, errors.New("crypto: at least one key is required")
	}
	b := &Box{aeads: map[string]cipher.AEAD{}}
	for i, k := range keys {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k))
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("crypto: key %d must be 32 bytes, base64-encoded (generate one with `openssl rand -base64 32`)", i+1)
		}
		block, err := aes.NewCipher(raw)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		id := fmt.Sprintf("%x", sum[:])[:keyIDLen]
		b.aeads[id] = aead
		if i == 0 {
			b.current = id
		}
	}
	return b, nil
}

// Seal encrypts plaintext. context binds the ciphertext to where it is
// stored (for example "server:12:password"), so a ciphertext copied into
// another row will not decrypt.
func (b *Box) Seal(plaintext []byte, context string) (string, error) {
	aead := b.aeads[b.current]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, plaintext, []byte(context))
	return "v1:" + b.current + ":" + base64.RawStdEncoding.EncodeToString(append(nonce, ct...)), nil
}

// Open decrypts a value produced by Seal with the same context.
func (b *Box) Open(sealed, context string) ([]byte, error) {
	parts := strings.SplitN(sealed, ":", 3)
	if len(parts) != 3 || parts[0] != "v1" {
		return nil, ErrDecrypt
	}
	aead, ok := b.aeads[parts[1]]
	if !ok {
		return nil, fmt.Errorf("%w: key %s is not configured", ErrDecrypt, parts[1])
	}
	raw, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(raw) < aead.NonceSize() {
		return nil, ErrDecrypt
	}
	pt, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(context))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// Current is the id of the key that seals new secrets.
func (b *Box) Current() string { return b.current }

// KeyID returns the key id stored on a sealed value, without decrypting it.
func KeyID(sealed string) (string, error) {
	parts := strings.SplitN(sealed, ":", 3)
	if len(parts) != 3 || parts[0] != "v1" || len(parts[1]) != keyIDLen {
		return "", ErrDecrypt
	}
	return parts[1], nil
}

// Reseal opens a secret and seals it with the current key. changed is false
// when it is already sealed with that key. A decrypt failure returns the
// original error and does not produce a replacement.
func (b *Box) Reseal(sealed, context string) (next string, changed bool, err error) {
	id, err := KeyID(sealed)
	if err != nil {
		return "", false, err
	}
	if id == b.current {
		return sealed, false, nil
	}
	pt, err := b.Open(sealed, context)
	if err != nil {
		return "", false, err
	}
	next, err = b.Seal(pt, context)
	if err != nil {
		return "", false, err
	}
	return next, true, nil
}
