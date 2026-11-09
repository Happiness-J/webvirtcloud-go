package console

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Claims struct {
	HostID string `json:"h"`
	VM     string `json:"v"`
	UserID string `json:"u"`
	Nonce  string `json:"n"`
	Expiry int64  `json:"e"`
	Scope  Scope  `json:"s"`
}

// Binding identifies the HTTP request a console token is allowed to
// authorize. Validate checks all three fields before the token is consumed.
type Binding struct {
	HostID string
	VM     string
	UserID string
}

type Scope string

const (
	ScopeWebSocket Scope = "ws"
	ScopePassword  Scope = "password"
)

var ErrBindingMismatch = errors.New("控制台令牌绑定不匹配")

type Tokens struct {
	key []byte
	ttl time.Duration
}

func NewTokens(key []byte, ttl time.Duration) (*Tokens, error) {
	if len(key) < 32 {
		return nil, errors.New("console token key must be at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("console token ttl must be positive")
	}
	return &Tokens{key: append([]byte(nil), key...), ttl: ttl}, nil
}

func (t *Tokens) Issue(hostID, vm, userID string, scope Scope, now time.Time) (string, error) {
	if !validScope(scope) {
		return "", errors.New("控制台令牌用途无效")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成控制台令牌: %w", err)
	}
	claims := Claims{
		HostID: hostID, VM: vm, UserID: userID,
		Nonce:  base64.RawURLEncoding.EncodeToString(nonce),
		Expiry: now.Add(t.ttl).Unix(), Scope: scope,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + t.sign(encoded), nil
}

// Validate verifies the token signature, purpose, expiry, and request binding.
// It does not consume the token; callers must atomically record its nonce
// before granting access.
func (t *Tokens) Validate(token string, scope Scope, binding Binding, now time.Time) (Claims, error) {
	if !validScope(scope) {
		return Claims{}, errors.New("控制台令牌用途无效")
	}
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok || encoded == "" || signature == "" {
		return Claims{}, errors.New("控制台令牌格式无效")
	}
	if !hmac.Equal([]byte(signature), []byte(t.sign(encoded))) {
		return Claims{}, errors.New("控制台令牌签名无效")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Claims{}, errors.New("控制台令牌编码无效")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, errors.New("控制台令牌内容无效")
	}
	if claims.HostID == "" || claims.VM == "" || claims.UserID == "" || claims.Nonce == "" || !validScope(claims.Scope) {
		return Claims{}, errors.New("控制台令牌缺少字段")
	}
	if claims.Scope != scope {
		return Claims{}, errors.New("控制台令牌用途不匹配")
	}
	if now.Unix() >= claims.Expiry {
		return Claims{}, errors.New("控制台令牌已过期")
	}
	if claims.HostID != binding.HostID || claims.VM != binding.VM || claims.UserID != binding.UserID {
		return Claims{}, ErrBindingMismatch
	}
	return claims, nil
}

func validScope(scope Scope) bool {
	return scope == ScopeWebSocket || scope == ScopePassword
}

func (t *Tokens) sign(payload string) string {
	mac := hmac.New(sha256.New, t.key)
	_, _ = mac.Write([]byte("wvc-console-v1\x00" + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (t *Tokens) TTL() time.Duration { return t.ttl }

func (t *Tokens) String() string { return fmt.Sprintf("console-tokens(ttl=%s)", t.ttl) }
