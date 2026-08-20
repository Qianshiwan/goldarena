// Package verify manages short-lived email verification codes used during
// account registration. Codes are single-use, expire after a TTL, and lock the
// email after too many failed attempts — combined with per-IP rate limiting in
// the caller this stops automated bulk registration.
package verify

import (
	"crypto/rand"
	"math/big"
	"sync"
	"time"
)

type record struct {
	code      string
	expiresAt time.Time
	attempts  int
}

// Store keeps pending verification codes keyed by email address.
type Store struct {
	mu    sync.Mutex
	codes map[string]*record
}

// NewStore creates an empty code store.
func NewStore() *Store {
	return &Store{codes: make(map[string]*record)}
}

// Generate creates a new 6-digit numeric code for email+purpose with the given TTL.
// purpose scopes the code (e.g. "register" vs "reset") so codes for different
// flows never collide on the same email address.
func (s *Store) Generate(email, purpose string, ttl time.Duration) string {
	code := randCode(6)
	key := email + "\n" + purpose
	s.mu.Lock()
	s.codes[key] = &record{code: code, expiresAt: time.Now().Add(ttl), attempts: 0}
	s.mu.Unlock()
	return code
}

// Check validates input against the stored code for email+purpose.
// Returns (true, "") on success (and consumes the code), or (false, reason)
// when the code is missing, expired, mismatched, or over the attempt limit.
func (s *Store) Check(email, purpose, input string, maxAttempts int) (bool, string) {
	key := email + "\n" + purpose
	s.mu.Lock()
	defer s.mu.Unlock()

	rec, ok := s.codes[key]
	if !ok {
		return false, "验证码不存在或已过期，请重新获取"
	}
	if time.Now().After(rec.expiresAt) {
		delete(s.codes, key)
		return false, "验证码已过期，请重新获取"
	}
	if rec.attempts >= maxAttempts {
		delete(s.codes, key)
		return false, "验证码错误次数过多，请重新获取"
	}
	if rec.code != input {
		rec.attempts++
		return false, "验证码错误"
	}
	delete(s.codes, key) // one-time use
	return true, ""
}

// randCode returns a zero-padded numeric code of the requested length.
func randCode(n int) string {
	const digits = "0123456789"
	b := make([]byte, n)
	max := big.NewInt(int64(len(digits)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			b[i] = '0'
			continue
		}
		b[i] = digits[v.Int64()]
	}
	return string(b)
}
