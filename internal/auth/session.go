package auth

import (
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

// Store is an in-memory session store (single process / single user).
type Store struct {
	mu       sync.Mutex
	byID     map[string]time.Time // id -> expiry
	ttl      time.Duration
	secure   bool
	password string
}

func NewStore(password string, ttl time.Duration, secure bool) *Store {
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
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
	// Constant-time compare; pad lengths carefully via subtle on equal-length slices.
	a := []byte(pw)
	b := []byte(s.password)
	if len(a) != len(b) {
		// still do a dummy compare to reduce timing leak on length
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
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
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
	// sliding expiration — keep you logged in while occasionally visiting
	s.byID[c.Value] = time.Now().Add(s.ttl)
	return true
}

// RefreshCookie rewrites the session cookie MaxAge so browsers keep it.
func (s *Store) RefreshCookie(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return
	}
	s.mu.Lock()
	exp, ok := s.byID[c.Value]
	s.mu.Unlock()
	if !ok {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    c.Value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure,
		Expires:  exp,
		MaxAge:   int(s.ttl.Seconds()),
	})
}

// ---- Brute-force guard (works even if attacker rotates IPs) ----

// LoginGuard rate-limits password attempts.
// - Global lockout after N failures (IP rotation cannot bypass)
// - Per-IP soft limit
// - Progressive delay on every attempt
type LoginGuard struct {
	mu sync.Mutex

	// global password failures for the single admin account
	globalFails   int
	globalLockUntil time.Time

	// per-IP attempts
	ipFails map[string]*ipBucket

	maxGlobalFails int
	globalLockFor  time.Duration
	maxIPFails     int
	ipWindow       time.Duration
	baseDelay      time.Duration
}

type ipBucket struct {
	fails   int
	window0 time.Time
}

func NewLoginGuard() *LoginGuard {
	return &LoginGuard{
		ipFails:        make(map[string]*ipBucket),
		maxGlobalFails: 8,                // after 8 wrong passwords...
		globalLockFor:  30 * time.Minute, // lock whole login for 30 min
		maxIPFails:     20,               // per IP soft cap
		ipWindow:       time.Hour,
		baseDelay:      400 * time.Millisecond,
	}
}

// Allow reports whether a login attempt may proceed. If not, returns retry-after duration.
func (g *LoginGuard) Allow(ip string) (ok bool, retryAfter time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked()

	now := time.Now()
	if now.Before(g.globalLockUntil) {
		return false, g.globalLockUntil.Sub(now)
	}
	ip = normalizeIP(ip)
	if b, exists := g.ipFails[ip]; exists {
		if now.Sub(b.window0) < g.ipWindow && b.fails >= g.maxIPFails {
			return false, g.ipWindow - now.Sub(b.window0)
		}
	}
	return true, 0
}

// Fail records a failed login (wrong password).
func (g *LoginGuard) Fail(ip string) (locked bool, retryAfter time.Duration) {
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
	// progressive delay hint for client messaging
	delay := time.Duration(b.fails) * g.baseDelay
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	return false, delay
}

// Success clears counters after a good password.
func (g *LoginGuard) Success(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.globalFails = 0
	g.globalLockUntil = time.Time{}
	delete(g.ipFails, normalizeIP(ip))
}

// Delay sleeps a minimum time to slow brute force (call on every attempt).
func (g *LoginGuard) Delay(ip string) {
	g.mu.Lock()
	fails := 0
	if b := g.ipFails[normalizeIP(ip)]; b != nil {
		fails = b.fails
	}
	// also scale with global fails
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
	// X-Forwarded-For may be "client, proxy"
	if i := strings.IndexByte(ip, ','); i >= 0 {
		ip = strings.TrimSpace(ip[:i])
	}
	host, _, err := net.SplitHostPort(ip)
	if err == nil {
		return host
	}
	return ip
}

// ClientIP extracts best-effort client IP behind Caddy.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return normalizeIP(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return normalizeIP(xri)
	}
	return normalizeIP(r.RemoteAddr)
}
