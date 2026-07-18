package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cashpulse/internal/auth"
)

// Auth holds tokens + session store for dual-role access control.
type Auth struct {
	IngestToken string
	AdminToken  string // optional machine token; web UI uses password only
	Sessions    *auth.Store
	Guard       *auth.LoginGuard
}

func tokenFromRequest(r *http.Request) string {
	authz := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(authz, prefix) {
		return strings.TrimSpace(authz[len(prefix):])
	}
	if t := r.Header.Get("X-API-Token"); t != "" {
		return t
	}
	// deliberately no query ?token= (leaks in logs/referrers)
	return ""
}

func constEq(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (a *Auth) isIngest(r *http.Request) bool {
	t := tokenFromRequest(r)
	return constEq(t, a.IngestToken)
}

func (a *Auth) isAdmin(r *http.Request) bool {
	if a.Sessions != nil && a.Sessions.Valid(r) {
		return true
	}
	t := tokenFromRequest(r)
	if constEq(t, a.AdminToken) {
		return true
	}
	return false
}

func withIngestAuth(a *Auth, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a == nil || !a.isIngest(r) {
			if a == nil || !a.isAdmin(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

func withAdminAuth(a *Auth, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a == nil || !a.isAdmin(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// slide cookie lifetime while using the app
		if a.Sessions != nil {
			a.Sessions.RefreshCookie(w, r)
		}
		next(w, r)
	}
}

// Login POST /api/v1/auth/login  {"password":"..."}
// Web UI uses password only. Brute-force is slowed + globally locked after failures.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || h.auth.Sessions == nil || !h.auth.Sessions.PasswordEnabled() {
		writeError(w, http.StatusBadRequest, "password login not configured (set ADMIN_PASSWORD)")
		return
	}
	if h.auth.Guard == nil {
		h.auth.Guard = auth.NewLoginGuard()
	}

	ip := auth.ClientIP(r)
	if ok, retry := h.auth.Guard.Allow(ip); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retry.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("登录暂时锁定，请 %d 分钟后再试", int(retry.Minutes())+1))
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	// Always delay — slows distributed brute force even with IP rotation.
	h.auth.Guard.Delay(ip)

	if !h.auth.Sessions.CheckPassword(body.Password) {
		locked, retry := h.auth.Guard.Fail(ip)
		if locked {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retry.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("密码错误次数过多，登录已锁定约 %d 分钟", int(retry.Minutes())+1))
			return
		}
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}

	h.auth.Guard.Success(ip)
	if _, err := h.auth.Sessions.Create(w); err != nil {
		writeError(w, http.StatusInternalServerError, "session error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"auth":       "session",
		"ttl_hours":  int((30 * 24 * time.Hour).Hours()), // informational; real TTL from server config
	})
}

// Logout POST /api/v1/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.auth != nil && h.auth.Sessions != nil {
		h.auth.Sessions.Destroy(w, r)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Me GET /api/v1/auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	ok := h.auth != nil && h.auth.isAdmin(r)
	mode := "none"
	if ok {
		if h.auth.Sessions != nil && h.auth.Sessions.Valid(r) {
			mode = "session"
			h.auth.Sessions.RefreshCookie(w, r)
		} else {
			mode = "token"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":     ok,
		"mode":              mode,
		"password_login":    h.auth != nil && h.auth.Sessions != nil && h.auth.Sessions.PasswordEnabled(),
		"ingest_configured": h.auth != nil && h.auth.IngestToken != "",
	})
}
