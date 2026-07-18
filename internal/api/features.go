package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"cashpulse/internal/store"
)

func (h *Handler) ExportTransactionsCSV(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" || to == "" {
		// default last 30 days in app timezone
		end := time.Now().In(h.svc.Location())
		start := end.AddDate(0, 0, -29)
		from = start.Format("2006-01-02")
		to = end.Format("2006-01-02")
	}
	items, err := h.svc.ExportTransactions(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=cashpulse_%s_%s.csv", from, to))
	// UTF-8 BOM for Excel
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "occurred_at", "direction", "kind", "amount", "merchant", "merchant_norm", "category", "card_last4", "bank", "balance_after", "person", "tags", "note"})
	for _, t := range items {
		tags := ""
		for i, tg := range t.Tags {
			if i > 0 {
				tags += "|"
			}
			tags += tg.Name
		}
		bal := ""
		if t.BalanceKnown {
			bal = fmt.Sprintf("%.2f", t.BalanceAfter)
		}
		_ = cw.Write([]string{
			strconv.FormatInt(t.ID, 10),
			t.OccurredAt.In(h.svc.Location()).Format("2006-01-02 15:04:05"),
			string(t.Direction),
			string(t.Kind),
			fmt.Sprintf("%.2f", t.Amount),
			t.Merchant,
			t.MerchantNorm,
			t.Category,
			t.CardLast4,
			t.Bank,
			bal,
			t.PersonName,
			tags,
			t.Note,
		})
	}
	cw.Flush()
}

func (h *Handler) Digest(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Digest(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) ListBudgets(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	items, err := h.svc.ListBudgets(r.Context(), month)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) UpsertBudget(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Month    string  `json:"month"`
		PersonID *int64  `json:"person_id"`
		Kind     string  `json:"kind"`
		Amount   float64 `json:"amount"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	b, err := h.svc.UpsertBudget(r.Context(), body.Month, body.PersonID, body.Kind, body.Amount)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (h *Handler) DeleteBudget(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteBudget(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListRules(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreateRule(w http.ResponseWriter, r *http.Request) {
	var body store.LabelRule
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Enabled = true
	out, err := h.svc.CreateRule(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *Handler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteRule(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListGoals(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListGoals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreateGoal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string  `json:"name"`
		Target float64 `json:"target"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	g, err := h.svc.CreateGoal(r.Context(), body.Name, body.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (h *Handler) DeleteGoal(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteGoal(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ListCards(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListCards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
