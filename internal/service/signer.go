package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// signer produces and verifies HMAC-SHA256 signed URL tokens of the form
// "expires:hex(hmac(purpose:id:expires))". Binding the purpose ("stream" vs
// "cover") prevents a token minted for one endpoint from being replayed on the
// other.
type signer struct {
	secret []byte
}

func newSigner(secret string) *signer {
	return &signer{secret: []byte(secret)}
}

func (s *signer) sign(purpose, id string, ttl int64) string {
	expires := time.Now().Unix() + ttl
	mac := hmac.New(sha256.New, s.secret)
	fmt.Fprintf(mac, "%s:%s:%d", purpose, id, expires)
	return fmt.Sprintf("%d:%s", expires, hex.EncodeToString(mac.Sum(nil)))
}

func (s *signer) verify(purpose, id, token string, now time.Time) bool {
	i := strings.IndexByte(token, ':')
	if i < 0 {
		return false
	}
	expires, err := strconv.ParseInt(token[:i], 10, 64)
	if err != nil || now.Unix() > expires {
		return false
	}
	mac := hmac.New(sha256.New, s.secret)
	fmt.Fprintf(mac, "%s:%s:%d", purpose, id, expires)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token[i+1:])) == 1
}
