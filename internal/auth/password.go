package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16

	minArgonMemory  = 8 * 1024
	maxArgonMemory  = 256 * 1024
	minArgonTime    = 1
	maxArgonTime    = 10
	minArgonThreads = 1
	maxArgonThreads = 16
	minArgonKeyLen  = 16
	maxArgonKeyLen  = 64
	minArgonSaltLen = 8
	maxArgonSaltLen = 64
)

var argonSlots = make(chan struct{}, 2)

// ConfigureArgon2 bounds concurrent password hashing for the whole process.
// The web handler uses the context-aware variant so queued requests can still
// be cancelled while the server is under password-spray load.
func ConfigureArgon2(concurrency int) {
	if concurrency < 1 {
		concurrency = 1
	}
	slots := make(chan struct{}, concurrency)
	argonSlots = slots
}

func acquireArgon2(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case argonSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseArgon2() { <-argonSlots }

func HashPassword(password string) (string, error) {
	return HashPasswordContext(context.Background(), password)
}

func HashPasswordContext(ctx context.Context, password string) (string, error) {
	if !utf8.ValidString(password) {
		return "", errors.New("password is not valid UTF-8")
	}
	if utf8.RuneCountInString(password) < 10 {
		return "", errors.New("password must be at least 10 characters")
	}
	if len(password) > 4096 {
		return "", errors.New("password is too long")
	}
	if !acquireArgon2(ctx) {
		return "", ctx.Err()
	}
	defer releaseArgon2()
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, uint8(argonThreads), argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password, encoded string) bool {
	return VerifyPasswordContext(context.Background(), password, encoded)
}

func VerifyPasswordContext(ctx context.Context, password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory uint32
	var iterations uint32
	var threads uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads)
	if err != nil || fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, threads) != parts[3] {
		return false
	}
	if memory < minArgonMemory || memory > maxArgonMemory ||
		iterations < minArgonTime || iterations > maxArgonTime ||
		threads < minArgonThreads || threads > maxArgonThreads {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	if len(salt) < minArgonSaltLen || len(salt) > maxArgonSaltLen ||
		len(want) < minArgonKeyLen || len(want) > maxArgonKeyLen {
		return false
	}
	if !acquireArgon2(ctx) {
		return false
	}
	defer releaseArgon2()
	// #nosec G115 -- len(want) was bounded to maxArgonKeyLen before this conversion.
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
