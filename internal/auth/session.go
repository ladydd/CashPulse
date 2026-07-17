package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const CookieName = "cashpulse_session"

// Store is an in-memory session store (single process / single user).
type Store struct {
	mu      sync.Mutex
	byID    map[string]time.Time // id -> expiry
	ttl     time.Duration
	secure  bool
	// password for login; empty means password login disabled
	password string
}

func NewStore(password string, ttl time.Duration, secure bool) *Store {
	return &Store{
		byID:     make(map[string]time.Time),
		ttl:      ttl,
		secure:   secure,
		password: password,
	}
}

func (s *Store) PasswordEnabled() bool { return s.password != "" }

func (s *Store) CheckPassword(pw string) bool {
	if s.password == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pw), []byte(s.password)) == 1
}

func (s *Store) Create(w http.ResponseWriter) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	exp := time.Now().Add(s.ttl)
	s.mu.Lock()
	s.byID[id] = exp
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure,
		Expires:  exp,
		MaxAge:   int(s.ttl.Seconds()),
	})
	return id, nil
}

func (s *Store) Destroy(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		s.mu.Lock()
		delete(s.byID, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Store) Valid(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.byID[c.Value]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.byID, c.Value)
		return false
	}
	// sliding expiration
	s.byID[c.Value] = time.Now().Add(s.ttl)
	return true
}
