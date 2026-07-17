package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"cashpulse/internal/auth"
)

// Auth holds tokens + session store for dual-role access control.
type Auth struct {
	IngestToken string
	AdminToken  string
	Sessions    *auth.Store
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
	return r.URL.Query().Get("token")
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
	// legacy: same token used for both
	if a.AdminToken == "" && constEq(t, a.IngestToken) {
		return true
	}
	return false
}

func withIngestAuth(a *Auth, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a == nil || !a.isIngest(r) {
			// admin session may also ingest (useful for web test tab)
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
		next(w, r)
	}
}

// Login POST /api/v1/auth/login  {"password":"..."}
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || h.auth.Sessions == nil || !h.auth.Sessions.PasswordEnabled() {
		writeError(w, http.StatusBadRequest, "password login not configured (set ADMIN_PASSWORD)")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// tiny delay against brute force
	time.Sleep(150 * time.Millisecond)
	if !h.auth.Sessions.CheckPassword(body.Password) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	if _, err := h.auth.Sessions.Create(w); err != nil {
		writeError(w, http.StatusInternalServerError, "session error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "auth": "session"})
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
		} else {
			mode = "token"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":    ok,
		"mode":             mode,
		"password_login":   h.auth != nil && h.auth.Sessions != nil && h.auth.Sessions.PasswordEnabled(),
		"ingest_configured": h.auth != nil && h.auth.IngestToken != "",
	})
}
