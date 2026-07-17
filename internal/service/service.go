package service

import (
	"context"
	"fmt"
	"time"

	"cashpulse/internal/model"
	"cashpulse/internal/parser"
	"cashpulse/internal/store"
)

// Service is the application use-case layer.
type Service struct {
	store  *store.Store
	parser *parser.Parser
	loc    *time.Location
}

func New(st *store.Store, p *parser.Parser, loc *time.Location) *Service {
	if loc == nil {
		loc = time.Local
	}
	return &Service{store: st, parser: p, loc: loc}
}

// IngestSMSRequest is the payload from the iPhone shortcut (or any client).
type IngestSMSRequest struct {
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
}

// IngestSMSResponse is returned after receiving an SMS.
type IngestSMSResponse struct {
	RawSMSID     int64              `json:"raw_sms_id"`
	Status       model.ParseStatus  `json:"status"`
	Transaction  *model.Transaction `json:"transaction,omitempty"`
	ParseError   string             `json:"parse_error,omitempty"`
	MatchedRule  string             `json:"matched_rule,omitempty"`
	Ignored      bool               `json:"ignored,omitempty"`
	IgnoreReason string             `json:"ignore_reason,omitempty"`
}

// IngestSMS stores the raw SMS and attempts to parse it into a transaction.
func (s *Service) IngestSMS(ctx context.Context, req IngestSMSRequest) (*IngestSMSResponse, error) {
	text := req.Text
	if text == "" {
		return nil, fmt.Errorf("text is required")
	}
	source := req.Source
	if source == "" {
		source = "shortcut"
	}

	id, err := s.store.InsertRawSMS(ctx, text, source)
	if err != nil {
		return nil, fmt.Errorf("save raw sms: %w", err)
	}

	resp := &IngestSMSResponse{RawSMSID: id}

	result, parseErr := s.parser.Parse(text, time.Now())
	if parseErr != nil {
		_ = s.store.MarkRawSMS(ctx, id, model.ParseStatusFailed, parseErr.Error())
		resp.Status = model.ParseStatusFailed
		resp.ParseError = parseErr.Error()
		return resp, nil
	}

	resp.MatchedRule = result.MatchedRule

	if result.Ignored {
		_ = s.store.MarkRawSMS(ctx, id, model.ParseStatusIgnored, result.IgnoreReason)
		resp.Status = model.ParseStatusIgnored
		resp.Ignored = true
		resp.IgnoreReason = result.IgnoreReason
		return resp, nil
	}

	txn := result.Transaction
	if txn == nil {
		_ = s.store.MarkRawSMS(ctx, id, model.ParseStatusFailed, "empty transaction")
		resp.Status = model.ParseStatusFailed
		resp.ParseError = "empty transaction"
		return resp, nil
	}

	txn.RawSMSID = id
	if txn.OccurredAt.IsZero() {
		txn.OccurredAt = time.Now()
	}
	txnID, err := s.store.InsertTransaction(ctx, txn)
	if err != nil {
		_ = s.store.MarkRawSMS(ctx, id, model.ParseStatusFailed, err.Error())
		return nil, fmt.Errorf("save transaction: %w", err)
	}
	txn.ID = txnID
	txn.CreatedAt = time.Now().UTC()

	if err := s.store.MarkRawSMS(ctx, id, model.ParseStatusOK, ""); err != nil {
		return nil, err
	}

	// auto-label rules (best-effort)
	_ = s.store.ApplyRulesToTxn(ctx, txnID)
	if updated, err := s.store.GetTransaction(ctx, txnID); err == nil {
		txn = &updated
	}

	resp.Status = model.ParseStatusOK
	resp.Transaction = txn
	return resp, nil
}

// ListTransactions returns paginated transactions with optional filters.
// from/to are local calendar dates YYYY-MM-DD; to is inclusive.
func (s *Service) ListTransactions(ctx context.Context, q string, limit, offset int, unlabeled bool, personID *int64, fromDate, toDate string) ([]model.Transaction, error) {
	f := store.TxnFilter{
		Q:         q,
		Limit:     limit,
		Offset:    offset,
		Unlabeled: unlabeled,
		PersonID:  personID,
	}
	if fromDate != "" {
		if t, err := time.ParseInLocation("2006-01-02", fromDate, s.loc); err == nil {
			f.From = &t
		}
	}
	if toDate != "" {
		if t, err := time.ParseInLocation("2006-01-02", toDate, s.loc); err == nil {
			end := t.AddDate(0, 0, 1)
			f.ToExclusive = &end
		}
	}
	return s.store.ListTransactionsFiltered(ctx, f)
}

// Dashboard builds the home page payload.
func (s *Service) Dashboard(ctx context.Context, days int) (*model.DashboardStats, error) {
	if days <= 0 {
		days = 7
	}
	today, err := s.store.DaySummary(ctx, time.Now().In(s.loc), s.loc)
	if err != nil {
		return nil, err
	}
	daily, err := s.store.DailySummaries(ctx, days, s.loc)
	if err != nil {
		return nil, err
	}
	unparsed, err := s.store.CountUnparsed(ctx)
	if err != nil {
		return nil, err
	}
	total, err := s.store.CountTransactions(ctx)
	if err != nil {
		return nil, err
	}
	unlabeled, err := s.store.CountUnlabeled(ctx)
	if err != nil {
		return nil, err
	}
	stats := &model.DashboardStats{
		Today:          today,
		Daily:          daily,
		UnparsedCount:  unparsed,
		TotalTxnCount:  total,
		UnlabeledCount: unlabeled,
	}
	if bal, at, ok, err := s.store.LatestBalance(ctx); err != nil {
		return nil, err
	} else if ok {
		stats.BalanceKnown = true
		stats.LatestBalance = bal
		stats.LatestBalanceAt = &at
	}
	return stats, nil
}

// ListUnparsed returns SMS that could not be parsed.
func (s *Service) ListUnparsed(ctx context.Context, limit int) ([]model.RawSMS, error) {
	return s.store.ListUnparsed(ctx, limit)
}

// Analytics returns multi-dimensional spend analysis for the UI.
// Prefer from/to (YYYY-MM-DD inclusive); days is fallback (0=all).
// kind: all|consume|transfer|refund|fee|income|other
func (s *Service) Analytics(ctx context.Context, days int, from, to, kind string) (*store.Analytics, error) {
	return s.store.Analytics(ctx, store.AnalyticsQuery{
		Days:     days,
		FromDate: from,
		ToDate:   to,
		Kind:     kind,
		Loc:      s.loc,
	})
}

func (s *Service) ListPeople(ctx context.Context) ([]model.Person, error) {
	return s.store.ListPeople(ctx)
}

func (s *Service) CreatePerson(ctx context.Context, name, color string) (model.Person, error) {
	return s.store.CreatePerson(ctx, name, color)
}

func (s *Service) DeletePerson(ctx context.Context, id int64) error {
	return s.store.DeletePerson(ctx, id)
}

func (s *Service) ListTags(ctx context.Context) ([]model.Tag, error) {
	return s.store.ListTags(ctx)
}

func (s *Service) CreateTag(ctx context.Context, name, color string) (model.Tag, error) {
	return s.store.CreateTag(ctx, name, color)
}

func (s *Service) DeleteTag(ctx context.Context, id int64) error {
	return s.store.DeleteTag(ctx, id)
}

// LabelTransactionRequest updates person and/or tags on one txn.
type LabelTransactionRequest struct {
	// PersonID: omit to leave unchanged; null JSON to clear; number to set.
	PersonID *int64  `json:"person_id"`
	PersonSet bool   `json:"-"`
	TagIDs   []int64 `json:"tag_ids"`
	TagsSet  bool    `json:"-"`
}

func (s *Service) LabelTransaction(ctx context.Context, txnID int64, req LabelTransactionRequest) (model.Transaction, error) {
	u := store.LabelUpdate{
		PersonSet: req.PersonSet,
		PersonID:  req.PersonID,
		TagsSet:   req.TagsSet,
		TagIDs:    req.TagIDs,
	}
	if u.TagIDs == nil && u.TagsSet {
		u.TagIDs = []int64{}
	}
	if err := s.store.UpdateTransactionLabels(ctx, txnID, u); err != nil {
		return model.Transaction{}, err
	}
	return s.store.GetTransaction(ctx, txnID)
}

// BulkLabelRequest assigns person (and optional tag) to many txns at once.
type BulkLabelRequest struct {
	// PersonID: set person; omit/null-clear only when PersonSet.
	PersonID  *int64 `json:"person_id"`
	PersonSet bool   `json:"-"`
	// TagID optional: add this tag to matches (does not replace other tags).
	TagID *int64 `json:"tag_id"`

	IDs           []int64 `json:"ids"`
	Merchant      string  `json:"merchant"`
	Category      string  `json:"category"`
	Direction     string  `json:"direction"` // out|in|""
	OnlyUnlabeled bool    `json:"only_unlabeled"`
}

type BulkLabelResult struct {
	PersonUpdated int64 `json:"person_updated"`
	TagUpdated    int64 `json:"tag_updated"`
}

func (s *Service) BulkLabel(ctx context.Context, req BulkLabelRequest) (BulkLabelResult, error) {
	f := store.BulkLabelFilter{
		IDs:           req.IDs,
		Merchant:      req.Merchant,
		Category:      req.Category,
		Direction:     req.Direction,
		OnlyUnlabeled: req.OnlyUnlabeled,
	}
	var out BulkLabelResult
	if req.PersonSet {
		n, err := s.store.BulkUpdatePerson(ctx, req.PersonID, f)
		if err != nil {
			return out, err
		}
		out.PersonUpdated = n
	}
	if req.TagID != nil {
		n, err := s.store.BulkAddTag(ctx, *req.TagID, f)
		if err != nil {
			return out, err
		}
		out.TagUpdated = n
	}
	if !req.PersonSet && req.TagID == nil {
		return out, fmt.Errorf("provide person_id and/or tag_id")
	}
	return out, nil
}

func (s *Service) UnlabeledByMerchant(ctx context.Context, limit int) ([]store.MerchantBucket, error) {
	return s.store.UnlabeledByMerchant(ctx, limit)
}

func (s *Service) ListBudgets(ctx context.Context, month string) ([]store.Budget, error) {
	return s.store.ListBudgets(ctx, month, s.loc)
}
func (s *Service) UpsertBudget(ctx context.Context, month string, personID *int64, kind string, amount float64) (store.Budget, error) {
	return s.store.UpsertBudget(ctx, month, personID, kind, amount)
}
func (s *Service) DeleteBudget(ctx context.Context, id int64) error {
	return s.store.DeleteBudget(ctx, id)
}
func (s *Service) ListRules(ctx context.Context) ([]store.LabelRule, error) {
	return s.store.ListRules(ctx)
}
func (s *Service) CreateRule(ctx context.Context, r store.LabelRule) (store.LabelRule, error) {
	return s.store.CreateRule(ctx, r)
}
func (s *Service) DeleteRule(ctx context.Context, id int64) error {
	return s.store.DeleteRule(ctx, id)
}
func (s *Service) ApplyRules(ctx context.Context, txnID int64) error {
	return s.store.ApplyRulesToTxn(ctx, txnID)
}
func (s *Service) ListGoals(ctx context.Context) ([]store.Goal, error) {
	return s.store.ListGoals(ctx)
}
func (s *Service) CreateGoal(ctx context.Context, name string, target float64) (store.Goal, error) {
	return s.store.CreateGoal(ctx, name, target)
}
func (s *Service) DeleteGoal(ctx context.Context, id int64) error {
	return s.store.DeleteGoal(ctx, id)
}
func (s *Service) ListCards(ctx context.Context) ([]store.CardInfo, error) {
	return s.store.ListCards(ctx)
}
func (s *Service) Digest(ctx context.Context) (*store.Digest, error) {
	return s.store.Digest(ctx, s.loc)
}
func (s *Service) ExportTransactions(ctx context.Context, fromDate, toDate string) ([]model.Transaction, error) {
	from, err := time.ParseInLocation("2006-01-02", fromDate, s.loc)
	if err != nil {
		return nil, fmt.Errorf("invalid from")
	}
	to, err := time.ParseInLocation("2006-01-02", toDate, s.loc)
	if err != nil {
		return nil, fmt.Errorf("invalid to")
	}
	return s.store.ExportTransactions(ctx, from, to.AddDate(0, 0, 1))
}
