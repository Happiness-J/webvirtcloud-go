package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	versionPrefix = "v1:"
	aadPrefix     = "v2:"
)

// Cipher encrypts small secrets such as libvirt URIs and future cloud
// credentials with AES-256-GCM. The key never lives in the database.
type Cipher struct {
	aead cipher.AEAD
	key  []byte
}

func LoadOrCreate(path string) (*Cipher, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("master key path is empty")
	}
	key, err := readKeyFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read master key: %w", err)
		}
		key, err = createKey(path)
		if err != nil {
			return nil, err
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(key)))
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("master key must be a base64 encoded 32-byte key")
	}
	return New(decoded)
}

func createKey(path string) ([]byte, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}
	if err := validateKeyDirectory(dir); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	encoded := []byte(base64.StdEncoding.EncodeToString(raw) + "\n")
	tmp, err := os.CreateTemp(dir, ".master.key-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary master key: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("chmod temporary master key: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("write temporary master key: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("sync temporary master key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temporary master key: %w", err)
	}
	// Link is atomic and never overwrites an existing key. If two replicas
	// race, exactly one link succeeds and the loser reads the winner's key.
	if err := os.Link(tmpName, path); err != nil {
		if !os.IsExist(err) {
			return nil, fmt.Errorf("install master key: %w", err)
		}
		existing, readErr := readKeyFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read concurrently created master key: %w", readErr)
		}
		return existing, nil
	}
	// #nosec G304 -- the key directory is an explicit operator configuration path.
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return encoded, nil
}

func readKeyFile(path string) ([]byte, error) {
	// O_NOFOLLOW closes the stat/read race and refuses a symlink even if an
	// attacker can briefly replace the path between lstat and open.
	// #nosec G304 -- the master-key path comes from trusted process configuration and is opened with O_NOFOLLOW.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat master key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("master key must be a regular file")
	}
	// #nosec G115 -- Unix process IDs are non-negative and fit in uint32 on supported platforms.
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("master key must be owned by the process user")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key %s must not be accessible by group or others", path)
	}
	if info.Size() > 4096 {
		return nil, errors.New("master key file is unexpectedly large")
	}
	if err := validateKeyDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	if len(data) > 4096 {
		return nil, errors.New("master key file is unexpectedly large")
	}
	return data, nil
}

func validateKeyDirectory(dir string) error {
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat master key directory: %w", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("master key directory %s must not be group/world writable", dir)
	}
	// #nosec G115 -- Unix process IDs are non-negative and fit in uint32 on supported platforms.
	if stat, ok := dirInfo.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("master key directory %s must be owned by the process user", dir)
	}
	return nil
}

func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256-GCM key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, key: append([]byte(nil), key...)}, nil
}

// DerivedKey returns an independent key for another purpose (for example
// console tokens) without reusing the AES-GCM key directly.
func (c *Cipher) DerivedKey(label string) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte("webvirtcloud-go:" + label))
	return mac.Sum(nil)
}

func (c *Cipher) Seal(plaintext string) (string, error) {
	return c.seal(versionPrefix, nil, plaintext)
}

// SealFor binds ciphertext to a purpose/identifier. This prevents a database
// attacker from swapping a ciphertext between hosts or jobs without detection.
func (c *Cipher) SealFor(purpose, plaintext string) (string, error) {
	if purpose == "" {
		return "", errors.New("cipher purpose is required")
	}
	return c.seal(aadPrefix, []byte(purpose), plaintext)
}

func (c *Cipher) seal(prefix string, aad []byte, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), aad)
	payload := append(nonce, sealed...)
	return prefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (c *Cipher) Open(ciphertext string) (string, error) {
	return c.open(nil, ciphertext)
}

// OpenFor opens ciphertext created by SealFor. Normal application paths only
// accept the purpose-bound v2 format. Legacy v1 values must be upgraded through
// OpenLegacy during startup before the web server or workers begin serving.
func (c *Cipher) OpenFor(purpose, ciphertext string) (string, error) {
	if purpose == "" {
		return "", errors.New("cipher purpose is required")
	}
	if strings.HasPrefix(ciphertext, versionPrefix) {
		return "", errors.New("legacy ciphertext is not purpose-bound and must be upgraded")
	}
	return c.open([]byte(purpose), ciphertext)
}

// OpenLegacy decrypts the pre-AAD v1 format. It is intentionally separate from
// OpenFor so replaying an old, unbound ciphertext cannot pass normal reads.
func (c *Cipher) OpenLegacy(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, versionPrefix) {
		return "", errors.New("ciphertext is not in the legacy v1 format")
	}
	return c.open(nil, ciphertext)
}

func (c *Cipher) open(aad []byte, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	prefix := versionPrefix
	if strings.HasPrefix(ciphertext, aadPrefix) {
		prefix = aadPrefix
	}
	if !strings.HasPrefix(ciphertext, prefix) {
		return "", errors.New("unsupported ciphertext version")
	}
	if prefix == aadPrefix && len(aad) == 0 {
		return "", errors.New("cipher purpose is required for v2 ciphertext")
	}
	// Legacy v1 payloads were sealed with no additional authenticated data.
	// Callers that still need to migrate them use OpenLegacy, while the normal
	// OpenFor path rejects them above.
	if prefix == versionPrefix {
		aad = nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(ciphertext, prefix))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns+c.aead.Overhead() {
		return "", errors.New("ciphertext is too short")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], aad)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return string(plain), nil
}
