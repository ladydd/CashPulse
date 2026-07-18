package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cashpulse/internal/model"
	"cashpulse/internal/service"
)

// Handler holds HTTP endpoints.
type Handler struct {
	svc  *service.Service
	auth *Auth
}

func NewHandler(svc *service.Service, auth *Auth) *Handler {
	return &Handler{svc: svc, auth: auth}
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// Health is a public liveness probe.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// IngestSMS accepts a full bank SMS from iPhone Shortcuts.
//
// Supported bodies:
//   - application/json: {"text":"...","source":"iphone"}
//   - text/plain: raw SMS body
//   - form field "text"
func (h *Handler) IngestSMS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	req, err := decodeIngestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := h.svc.IngestSMS(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	code := http.StatusCreated
	if resp.Duplicate {
		code = http.StatusOK
	}
	if resp.Status == model.ParseStatusPending {
		code = http.StatusAccepted // 202
	}
	writeJSON(w, code, resp)
}

func decodeIngestBody(r *http.Request) (service.IngestSMSRequest, error) {
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.Contains(ct, "application/json"):
		var req service.IngestSMSRequest
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		if err := dec.Decode(&req); err != nil {
			return req, err
		}
		return req, nil
	case strings.Contains(ct, "application/x-www-form-urlencoded"):
		if err := r.ParseForm(); err != nil {
			return service.IngestSMSRequest{}, err
		}
		return service.IngestSMSRequest{
			Text:   r.FormValue("text"),
			Source: r.FormValue("source"),
		}, nil
	default:
		// plain text or unknown — treat body as SMS text
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return service.IngestSMSRequest{}, err
		}
		return service.IngestSMSRequest{Text: string(b)}, nil
	}
}

// ListTransactions returns recent structured transactions.
// Query: q, limit, offset, unlabeled=1, person_id=, from=YYYY-MM-DD, to=YYYY-MM-DD (inclusive)
func (h *Handler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	q := r.URL.Query().Get("q")
	unlabeled := r.URL.Query().Get("unlabeled") == "1" || r.URL.Query().Get("unlabeled") == "true"
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")
	var personID *int64
	if v := r.URL.Query().Get("person_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			personID = &n
		}
	}

	items, total, err := h.svc.ListTransactions(r.Context(), q, limit, offset, unlabeled, personID, fromDate, toDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if items == nil {
		items = []model.Transaction{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// Dashboard returns today + recent daily summaries.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	stats, err := h.svc.Dashboard(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ListUnparsed returns SMS that failed parsing (for rule tuning).
func (h *Handler) ListUnparsed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.ListUnparsed(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []model.RawSMS{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// Analytics returns channel / category / monthly analysis.
// Query priority:
//   - from=YYYY-MM-DD&to=YYYY-MM-DD (inclusive)
//   - month=YYYY-MM  (whole calendar month)
//   - days=7|30|90|0|all
func (h *Handler) Analytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if m := r.URL.Query().Get("month"); m != "" && from == "" && to == "" {
		// whole month
		t, err := time.Parse("2006-01", m)
		if err == nil {
			from = t.Format("2006-01-02")
			to = t.AddDate(0, 1, -1).Format("2006-01-02")
		}
	}
	days := 0
	if from == "" && to == "" {
		q := r.URL.Query().Get("days")
		days = 30
		if q == "all" || q == "0" {
			days = 0
		} else if q != "" {
			if n, err := strconv.Atoi(q); err == nil {
				days = n
			}
		}
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "consume" // default: 日常消费，剔除大额转账噪音
	}
	stats, err := h.svc.Analytics(r.Context(), days, from, to, kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) ListPeople(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListPeople(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreatePerson(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := h.svc.CreatePerson(r.Context(), body.Name, body.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *Handler) DeletePerson(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeletePerson(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListTags(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListTags(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	t, err := h.svc.CreateTag(r.Context(), body.Name, body.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *Handler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteTag(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LabelTransaction PATCH /api/v1/transactions/{id}/labels
// Body: {"person_id": 1|null, "tag_ids": [1,2]} — omit fields to leave unchanged.
func (h *Handler) LabelTransaction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req := service.LabelTransactionRequest{}
	if v, ok := rawMap["person_id"]; ok {
		req.PersonSet = true
		if string(v) != "null" {
			var pid int64
			if err := json.Unmarshal(v, &pid); err != nil {
				writeError(w, http.StatusBadRequest, "invalid person_id")
				return
			}
			req.PersonID = &pid
		}
	}
	if v, ok := rawMap["tag_ids"]; ok {
		req.TagsSet = true
		if err := json.Unmarshal(v, &req.TagIDs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid tag_ids")
			return
		}
	}
	if !req.PersonSet && !req.TagsSet {
		writeError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	txn, err := h.svc.LabelTransaction(r.Context(), id, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, txn)
}

func pathID(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(r.PathValue(key), 10, 64)
}

// BulkLabel POST /api/v1/labels/bulk
// Assign person/tag to many transactions by ids or merchant/category.
func (h *Handler) BulkLabel(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body")
		return
	}
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	var body struct {
		IDs           []int64 `json:"ids"`
		Merchant      string  `json:"merchant"`
		Category      string  `json:"category"`
		Direction     string  `json:"direction"`
		OnlyUnlabeled bool    `json:"only_unlabeled"`
		TagID         *int64  `json:"tag_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req := service.BulkLabelRequest{
		IDs:           body.IDs,
		Merchant:      body.Merchant,
		Category:      body.Category,
		Direction:     body.Direction,
		OnlyUnlabeled: body.OnlyUnlabeled,
		TagID:         body.TagID,
	}
	if v, ok := rawMap["person_id"]; ok {
		req.PersonSet = true
		if string(v) != "null" {
			var pid int64
			if err := json.Unmarshal(v, &pid); err != nil {
				writeError(w, http.StatusBadRequest, "invalid person_id")
				return
			}
			req.PersonID = &pid
		}
	}
	res, err := h.svc.BulkLabel(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// UnlabeledMerchants GET /api/v1/labels/unlabeled-merchants
func (h *Handler) UnlabeledMerchants(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.UnlabeledByMerchant(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
