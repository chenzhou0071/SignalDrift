package lobby

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TokenIssuer struct {
	secret []byte
	ttl    int64
	now    func() int64
}

func NewTokenIssuer(secret string, ttlSec int64, now func() int64) *TokenIssuer {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &TokenIssuer{secret: []byte(secret), ttl: ttlSec, now: now}
}

func (t *TokenIssuer) sign(payload string) string {
	m := hmac.New(sha256.New, t.secret)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func (t *TokenIssuer) Issue(uid int64) string {
	payload := fmt.Sprintf("%d.%d", uid, t.now()+t.ttl)
	return payload + "." + t.sign(payload)
}

func (t *TokenIssuer) Verify(token string) (int64, bool) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return 0, false
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(t.sign(payload)), []byte(parts[2])) {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || t.now() > exp {
		return 0, false
	}
	uid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return uid, true
}
