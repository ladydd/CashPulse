package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// NewRouter wires API routes and optional static frontend.
func NewRouter(h *Handler, staticFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /api/v1/health", h.Health)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.Logout)
	mux.HandleFunc("GET /api/v1/auth/me", h.Me)

	// Ingest only (iPhone Shortcut) — INGEST_TOKEN or admin session
	mux.HandleFunc("POST /api/v1/sms", withIngestAuth(h.auth, h.IngestSMS))

	// Admin (session cookie or ADMIN_TOKEN / legacy API_TOKEN)
	admin := func(fn http.HandlerFunc) http.HandlerFunc {
		return withAdminAuth(h.auth, fn)
	}
	mux.HandleFunc("GET /api/v1/transactions", admin(h.ListTransactions))
	mux.HandleFunc("PATCH /api/v1/transactions/{id}/labels", admin(h.LabelTransaction))
	mux.HandleFunc("POST /api/v1/labels/bulk", admin(h.BulkLabel))
	mux.HandleFunc("GET /api/v1/labels/unlabeled-merchants", admin(h.UnlabeledMerchants))
	mux.HandleFunc("GET /api/v1/dashboard", admin(h.Dashboard))
	mux.HandleFunc("GET /api/v1/analytics", admin(h.Analytics))
	mux.HandleFunc("GET /api/v1/unparsed", admin(h.ListUnparsed))
	mux.HandleFunc("GET /api/v1/people", admin(h.ListPeople))
	mux.HandleFunc("POST /api/v1/people", admin(h.CreatePerson))
	mux.HandleFunc("DELETE /api/v1/people/{id}", admin(h.DeletePerson))
	mux.HandleFunc("GET /api/v1/tags", admin(h.ListTags))
	mux.HandleFunc("POST /api/v1/tags", admin(h.CreateTag))
	mux.HandleFunc("DELETE /api/v1/tags/{id}", admin(h.DeleteTag))
	mux.HandleFunc("GET /api/v1/export/transactions.csv", admin(h.ExportTransactionsCSV))
	mux.HandleFunc("GET /api/v1/digest", admin(h.Digest))
	mux.HandleFunc("GET /api/v1/budgets", admin(h.ListBudgets))
	mux.HandleFunc("PUT /api/v1/budgets", admin(h.UpsertBudget))
	mux.HandleFunc("DELETE /api/v1/budgets/{id}", admin(h.DeleteBudget))
	mux.HandleFunc("GET /api/v1/rules", admin(h.ListRules))
	mux.HandleFunc("POST /api/v1/rules", admin(h.CreateRule))
	mux.HandleFunc("DELETE /api/v1/rules/{id}", admin(h.DeleteRule))
	mux.HandleFunc("GET /api/v1/goals", admin(h.ListGoals))
	mux.HandleFunc("POST /api/v1/goals", admin(h.CreateGoal))
	mux.HandleFunc("DELETE /api/v1/goals/{id}", admin(h.DeleteGoal))
	mux.HandleFunc("GET /api/v1/cards", admin(h.ListCards))

	if staticFS != nil {
		fileServer := http.FileServer(http.FS(staticFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				path = "index.html"
			}
			if _, err := fs.Stat(staticFS, path); err != nil {
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	return withLogging(withCORS(mux))
}
