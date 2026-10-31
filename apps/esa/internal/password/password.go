// Package password hashes and checks passwords with argon2id.
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters from the OWASP password storage cheat sheet
// (19 MiB, 2 iterations, 1 lane).
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// ErrBusy means too many hashes are already running. Callers should answer
// 503 or ask the user to try again.
var ErrBusy = errors.New("password: too many sign-ins at once, try again")

// maxWait is how long a hash waits for a free slot before giving up.
const maxWait = 5 * time.Second

// slots bounds concurrent argon2 work, so a flood of sign-ins cannot use
// unbounded memory (19 MiB each) or CPU.
var slots = make(chan struct{}, runtime.NumCPU())

func acquire(ctx context.Context) error {
	t := time.NewTimer(maxWait)
	defer t.Stop()
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ErrBusy
	}
}

func release() { <-slots }

// Hash returns a PHC-format argon2id hash.
func Hash(ctx context.Context, password string) (string, error) {
	if err := acquire(ctx); err != nil {
		return "", err
	}
	defer release()
	return newHash(password)
}

func newHash(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var errBadHash = errors.New("password: malformed hash")

// Check reports whether password matches hash, in constant time.
func Check(ctx context.Context, hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var mem uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	if err := acquire(ctx); err != nil {
		return false, err
	}
	defer release()
	got := argon2.IDKey([]byte(password), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Dummy is a valid hash of a throwaway password. Check it when the account
// does not exist, so a failed sign-in takes the same time either way.
var Dummy = func() string {
	h, _ := newHash("esa-dummy-password-for-timing")
	return h
}()
