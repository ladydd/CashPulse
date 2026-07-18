package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CookieName = "cashpulse_session"

// SessionBackend persists sessions (e.g. SQLite).
type SessionBackend interface {
	SaveSession(ctx context.Context, id string, expiresAt time.Time) error
	GetSessionExpiry(ctx context.Context, id string) (time.Time, bool, error)
	DeleteSession(ctx context.Context, id string) error
}

// Store is session + password + brute-force guard for admin web login.
type Store struct {
	mu       sync.Mutex
	mem      map[string]time.Time // fallback if backend nil
	backend  SessionBackend
	ttl      time.Duration
	secure   bool
	password string
}

func NewStore(password string, ttl time.Duration, secure bool, backend SessionBackend) *Store {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Store{
		mem:      make(map[string]time.Time),
		backend:  backend,
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
	a, b := []byte(pw), []byte(s.password)
	if len(a) != len(b) {
		subtle.ConstantTimeCompare(b, b)
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func (s *Store) Create(w http.ResponseWriter) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	exp := time.Now().Add(s.ttl)
	if s.backend != nil {
		if err := s.backend.SaveSession(context.Background(), id, exp); err != nil {
			return "", err
		}
	} else {
		s.mu.Lock()
		s.mem[id] = exp
		s.mu.Unlock()
	}
	s.writeCookie(w, id, exp)
	return id, nil
}

func (s *Store) Destroy(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if s.backend != nil {
			_ = s.backend.DeleteSession(context.Background(), c.Value)
		} else {
			s.mu.Lock()
			delete(s.mem, c.Value)
			s.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.secure, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(0, 0),
	})
}

func (s *Store) Valid(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false
	}
	id := c.Value
	var exp time.Time
	var ok bool
	if s.backend != nil {
		exp, ok, err = s.backend.GetSessionExpiry(context.Background(), id)
		if err != nil || !ok {
			return false
		}
	} else {
		s.mu.Lock()
		exp, ok = s.mem[id]
		if !ok || time.Now().After(exp) {
			if ok {
				delete(s.mem, id)
			}
			s.mu.Unlock()
			return false
		}
		s.mu.Unlock()
	}
	// sliding expiry
	newExp := time.Now().Add(s.ttl)
	if s.backend != nil {
		_ = s.backend.SaveSession(context.Background(), id, newExp)
	} else {
		s.mu.Lock()
		s.mem[id] = newExp
		s.mu.Unlock()
	}
	return true
}

func (s *Store) RefreshCookie(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return
	}
	// Valid already slides backend; just re-emit cookie
	exp := time.Now().Add(s.ttl)
	s.writeCookie(w, c.Value, exp)
}

func (s *Store) writeCookie(w http.ResponseWriter, id string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: id, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secure,
		Expires: exp, MaxAge: int(s.ttl.Seconds()),
	})
}

// ---- LoginGuard (global + per-IP) ----

type LoginGuard struct {
	mu              sync.Mutex
	globalFails     int
	globalLockUntil time.Time
	ipFails         map[string]*ipBucket
	maxGlobalFails  int
	globalLockFor   time.Duration
	maxIPFails      int
	ipWindow        time.Duration
	baseDelay       time.Duration
}

type ipBucket struct {
	fails   int
	window0 time.Time
}

func NewLoginGuard() *LoginGuard {
	return &LoginGuard{
		ipFails:        make(map[string]*ipBucket),
		maxGlobalFails: 8,
		globalLockFor:  30 * time.Minute,
		maxIPFails:     20,
		ipWindow:       time.Hour,
		baseDelay:      400 * time.Millisecond,
	}
}

func (g *LoginGuard) Allow(ip string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked()
	now := time.Now()
	if now.Before(g.globalLockUntil) {
		return false, g.globalLockUntil.Sub(now)
	}
	ip = normalizeIP(ip)
	if b, ok := g.ipFails[ip]; ok {
		if now.Sub(b.window0) < g.ipWindow && b.fails >= g.maxIPFails {
			return false, g.ipWindow - now.Sub(b.window0)
		}
	}
	return true, 0
}

func (g *LoginGuard) Fail(ip string) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	ip = normalizeIP(ip)
	b := g.ipFails[ip]
	if b == nil || now.Sub(b.window0) >= g.ipWindow {
		b = &ipBucket{window0: now}
		g.ipFails[ip] = b
	}
	b.fails++
	g.globalFails++
	if g.globalFails >= g.maxGlobalFails {
		g.globalLockUntil = now.Add(g.globalLockFor)
		g.globalFails = 0
		return true, g.globalLockFor
	}
	d := time.Duration(b.fails) * g.baseDelay
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return false, d
}

func (g *LoginGuard) Success(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.globalFails = 0
	g.globalLockUntil = time.Time{}
	delete(g.ipFails, normalizeIP(ip))
}

func (g *LoginGuard) Delay(ip string) {
	g.mu.Lock()
	fails := 0
	if b := g.ipFails[normalizeIP(ip)]; b != nil {
		fails = b.fails
	}
	gf := g.globalFails
	g.mu.Unlock()
	d := g.baseDelay + time.Duration(fails+gf)*150*time.Millisecond
	if d > 3*time.Second {
		d = 3 * time.Second
	}
	time.Sleep(d)
}

func (g *LoginGuard) cleanupLocked() {
	now := time.Now()
	for ip, b := range g.ipFails {
		if now.Sub(b.window0) > g.ipWindow*2 {
			delete(g.ipFails, ip)
		}
	}
}

func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	if i := strings.IndexByte(ip, ','); i >= 0 {
		ip = strings.TrimSpace(ip[:i])
	}
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		return host
	}
	return ip
}

func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return normalizeIP(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return normalizeIP(xri)
	}
	return normalizeIP(r.RemoteAddr)
}
