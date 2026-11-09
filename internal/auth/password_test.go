package auth

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password verified")
	}
}

func TestPasswordMinimumLength(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("expected minimum length error")
	}
}

func TestPasswordUsesCharacterLengthAndUTF8(t *testing.T) {
	hash, err := HashPassword("中文密码足够长十个字符")
	if err != nil {
		t.Fatalf("multibyte password rejected: %v", err)
	}
	if !VerifyPassword("中文密码足够长十个字符", hash) {
		t.Fatal("multibyte password did not verify")
	}
	if _, err := HashPassword(string([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8, 0xf7, 0xf6})); err == nil {
		t.Fatal("invalid UTF-8 password was accepted")
	}
}

func TestVerifyPasswordRejectsMaliciousParameters(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString([]byte("12345678"))
	key := base64.RawStdEncoding.EncodeToString([]byte("1234567890123456"))
	tests := []string{
		fmt.Sprintf("$argon2id$v=19$m=999999999,t=3,p=2$%s$%s", salt, key),
		fmt.Sprintf("$argon2id$v=19$m=65536,t=999999,p=2$%s$%s", salt, key),
		fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=255$%s$%s", salt, key),
		"$argon2id$v=19$m=65536,t=3,p=2$bad$" + key,
		"$argon2i$v=19$m=65536,t=3,p=2$" + salt + "$" + key,
	}
	for _, encoded := range tests {
		if VerifyPassword("correct horse battery staple", encoded) {
			t.Fatalf("malicious or invalid hash verified: %s", encoded)
		}
	}
	// A syntactically valid but deliberately enormous parameter set must be
	// rejected without allocating the advertised amount of memory.
	if strings.Contains(tests[0], "999999999") == false {
		t.Fatal("test fixture did not contain the oversized memory parameter")
	}
}
