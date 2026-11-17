package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP uses HMAC-SHA1
	"encoding/base32"
	"encoding/binary"
	"strings"
	"time"
)

const totpDigits = 6
const totpStep = 30 * time.Second

func newTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func totpAt(secret string, at time.Time) (string, bool) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) < 10 {
		return "", false
	}
	seconds := at.Unix()
	if seconds < 0 {
		return "", false
	}
	counter := uint64(seconds) / uint64(totpStep/time.Second)
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code := value % 1000000
	return pad6(code), true
}

func pad6(v uint32) string {
	s := "000000" + itoa(v)
	return s[len(s)-6:]
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// validTOTP accepts the current step and one step on either side.
func validTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits || secret == "" {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	for _, delta := range []time.Duration{-totpStep, 0, totpStep} {
		got, ok := totpAt(secret, now.Add(delta))
		if ok && hmac.Equal([]byte(got), []byte(code)) {
			return true
		}
	}
	return false
}
