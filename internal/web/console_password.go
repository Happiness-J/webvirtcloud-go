package web

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

func newConsolePassword() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(raw), "="), nil
}

func graphicsArg(password string) string {
	return "vnc,listen=127.0.0.1,password=" + password
}
