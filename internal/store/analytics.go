package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"cashpulse/internal/model"
)

// AnalyticsQuery controls the analytics window.
// Priority: FromDate+ToDate (inclusive local calendar days) > Days > all-time.
type AnalyticsQuery struct {
	// Days > 0: last N local days including today.
	// Days <= 0 and no From/To: all time.
	Days int
	// FromDate / ToDate as YYYY-MM-DD in Loc (inclusive).
	FromDate string
	ToDate   string
	// Kind filters economic type: consume|transfer|refund|fee|income|other|"" (all).
	Kind string
	Loc  *time.Location
}

// ChannelStat is spend aggregated by merchant/channel.
type ChannelStat struct {
	Name     string  `json:"name"`
	Expense  float64 `json:"expense"`
	Income   float64 `json:"income"`
	TxnCount int     `json:"txn_count"`
	Pct      float64 `json:"pct"` // share of total expense in range
}

// CategoryStat aggregates by category.
type CategoryStat struct {
	Name     string  `json:"name"`
	Expense  float64 `json:"expense"`
	Income   float64 `json:"income"`
	TxnCount int     `json:"txn_count"`
	Pct      float64 `json:"pct"`
}

// MonthlyStat is calendar-month rollup in local time.
type MonthlyStat struct {
	Month    string  `json:"month"` // YYYY-MM
	Expense  float64 `json:"expense"`
	Income   float64 `json:"income"`
	Net      float64 `json:"net"`
	TxnCount int     `json:"txn_count"`
}

// BalancePoint is one balance observation from SMS.
type BalancePoint struct {
	At      time.Time `json:"at"`
	Balance float64   `json:"balance"`
}

// BalanceHealth summarizes runway / low-balance risk for the selected window.
type BalanceHealth struct {
	MinBalance     float64    `json:"min_balance"`
	AvgBalance     float64    `json:"avg_balance"`
	MaxBalance     float64    `json:"max_balance"`
	SampleCount    int        `json:"sample_count"`
	// DaysOfRunway = latest_balance / avg_daily_expense (consume); 0 if unknown.
	DaysOfRunway   float64    `json:"days_of_runway"`
	LowThreshold   float64    `json:"low_threshold"` // e.g. 100
	LowHitCount    int        `json:"low_hit_count"`
	LastLowAt      *time.Time `json:"last_low_at,omitempty"`
}

// RangeTotals is the headline numbers for a window.
type RangeTotals struct {
	Expense         float64 `json:"expense"`
	Income          float64 `json:"income"`
	Net             float64 `json:"net"`
	TxnCount        int     `json:"txn_count"`
	ExpenseCount    int     `json:"expense_count"`
	IncomeCount     int     `json:"income_count"`
	AvgDailyExpense float64 `json:"avg_daily_expense"`
	ActiveDays      int     `json:"active_days"`
}

// Analytics is the full analysis payload for the dashboard.
type Analytics struct {
	From            string               `json:"from"`
	To              string               `json:"to"`
	Days            int                  `json:"days"`
	Kind            string               `json:"kind"` // filter applied: all|consume|...
	Granularity     string               `json:"granularity"` // day | month
	Today           model.TodaySummary   `json:"today"`
	ThisMonth       RangeTotals          `json:"this_month"`
	Range           RangeTotals          `json:"range"`
	PrevRange       RangeTotals          `json:"prev_range"` // equal-length period before From
	// AllKinds totals for the same window without kind filter (for context chips).
	RangeAllKinds   RangeTotals          `json:"range_all_kinds"`
	Daily           []model.DailySummary `json:"daily"`
	Monthly         []MonthlyStat        `json:"monthly"`
	ByChannel       []ChannelStat        `json:"by_channel"`
	ByCategory      []CategoryStat       `json:"by_category"`
	ByKind          []ChannelStat        `json:"by_kind"` // reuse ChannelStat shape: Name=kind label
	ByPerson        []model.PersonStat   `json:"by_person"`
	TopExpenses     []model.Transaction  `json:"top_expenses"`
	RecentIncome    []model.Transaction  `json:"recent_income"`
	BalanceSeries   []BalancePoint       `json:"balance_series"`
	BalanceHealth   BalanceHealth        `json:"balance_health"`
	LatestBalance   float64              `json:"latest_balance,omitempty"`
	LatestBalanceAt *time.Time           `json:"latest_balance_at,omitempty"`
	LatestCardLast4 string              `json:"latest_card_last4,omitempty"`
	BalanceKnown    bool                 `json:"balance_known"`
	TotalTxnCount   int                  `json:"total_txn_count"`
	UnparsedCount   int                  `json:"unparsed_count"`
	UnlabeledCount  int                  `json:"unlabeled_count"`
}

func (s *Store) Analytics(ctx context.Context, q AnalyticsQuery) (*Analytics, error) {
	if q.Loc == nil {
		q.Loc = time.Local
	}
	now := time.Now().In(q.Loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, q.Loc)

	from, toDay, err := resolveAnalyticsWindow(q, todayStart, s)
	if err != nil {
		return nil, err
	}
	toEnd := toDay.AddDate(0, 0, 1)
	days := int(toDay.Sub(from).Hours()/24) + 1
	if days < 1 {
		days = 1
	}

	granularity := "day"
	if days > 92 {
		granularity = "month"
	}

	kind := q.Kind
	if kind == "" {
		kind = "all"
	}
	out := &Analytics{
		From:        from.Format("2006-01-02"),
		To:          toDay.Format("2006-01-02"),
		Days:        days,
		Kind:        kind,
		Granularity: granularity,
	}

	// Today
	today, err := s.DaySummary(ctx, now, q.Loc)
	if err != nil {
		return nil, err
	}
	out.Today = today

	// This month (calendar) — always all kinds for the chip
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, q.Loc)
	monthEnd := todayStart.AddDate(0, 0, 1)
	monthTot, err := s.rangeTotals(ctx, monthStart, monthEnd, q.Loc, "all")
	if err != nil {
		return nil, err
	}
	out.ThisMonth = monthTot

	// Selected range (filtered) + unfiltered context
	rangeAll, err := s.rangeTotals(ctx, from, toEnd, q.Loc, "all")
	if err != nil {
		return nil, err
	}
	out.RangeAllKinds = rangeAll
	rangeTot, err := s.rangeTotals(ctx, from, toEnd, q.Loc, kind)
	if err != nil {
		return nil, err
	}
	out.Range = rangeTot

	// Previous equal-length window for comparison (same kind filter)
	prevTo := from
	prevFrom := from.AddDate(0, 0, -days)
	prevTot, err := s.rangeTotals(ctx, prevFrom, prevTo, q.Loc, kind)
	if err != nil {
		return nil, err
	}
	out.PrevRange = prevTot

	// Daily: full selected window (up to ~400 days)
	dailyDays := days
	if dailyDays > 400 {
		fromDaily := toDay.AddDate(0, 0, -(400 - 1))
		daily, err := s.DailySummariesFrom(ctx, fromDaily, 400, q.Loc, kind)
		if err != nil {
			return nil, err
		}
		out.Daily = daily
	} else {
		daily, err := s.DailySummariesFrom(ctx, from, dailyDays, q.Loc, kind)
		if err != nil {
			return nil, err
		}
		out.Daily = daily
	}

	monthly, err := s.monthlyStats(ctx, from, toEnd, q.Loc, kind)
	if err != nil {
		return nil, err
	}
	out.Monthly = monthly

	byCh, err := s.channelStats(ctx, from, toEnd, rangeTot.Expense, kind)
	if err != nil {
		return nil, err
	}
	out.ByChannel = byCh

	byCat, err := s.categoryStats(ctx, from, toEnd, rangeTot.Expense, kind)
	if err != nil {
		return nil, err
	}
	out.ByCategory = byCat

	byKind, err := s.kindStats(ctx, from, toEnd, rangeAll.Expense+rangeAll.Income)
	if err != nil {
		return nil, err
	}
	out.ByKind = byKind

	byPerson, err := s.PersonStats(ctx, from, toEnd, rangeTot.Expense, kind)
	if err != nil {
		return nil, err
	}
	out.ByPerson = byPerson

	top, err := s.topExpenses(ctx, from, toEnd, 10)
	if err != nil {
		return nil, err
	}
	out.TopExpenses = top

	inc, err := s.recentByDirection(ctx, from, toEnd, model.DirectionIn, 8)
	if err != nil {
		return nil, err
	}
	out.RecentIncome = inc

	balSeries, err := s.balanceSeries(ctx, from, toEnd, 120)
	if err != nil {
		return nil, err
	}
	out.BalanceSeries = balSeries

	// Balance health uses all balance points in range + consume avg spend for runway.
	consumeTot, err := s.rangeTotals(ctx, from, toEnd, q.Loc, string(model.KindConsume))
	if err != nil {
		return nil, err
	}
	out.BalanceHealth = s.computeBalanceHealth(balSeries, consumeTot.AvgDailyExpense, 100)

	if bal, at, card, ok, err := s.LatestBalanceDetail(ctx); err != nil {
		return nil, err
	} else if ok {
		out.BalanceKnown = true
		out.LatestBalance = bal
		out.LatestBalanceAt = &at
		out.LatestCardLast4 = card
	}

	if n, err := s.CountTransactions(ctx); err != nil {
		return nil, err
	} else {
		out.TotalTxnCount = n
	}
	if n, err := s.CountUnparsed(ctx); err != nil {
		return nil, err
	} else {
		out.UnparsedCount = n
	}
	if n, err := s.CountUnlabeled(ctx); err != nil {
		return nil, err
	} else {
		out.UnlabeledCount = n
	}

	return out, nil
}

func resolveAnalyticsWindow(q AnalyticsQuery, todayStart time.Time, s *Store) (from, toDay time.Time, err error) {
	loc := q.Loc
	// Explicit inclusive range
	if q.FromDate != "" && q.ToDate != "" {
		from, err = time.ParseInLocation("2006-01-02", q.FromDate, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid from date")
		}
		toDay, err = time.ParseInLocation("2006-01-02", q.ToDate, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid to date")
		}
		if toDay.Before(from) {
			from, toDay = toDay, from
		}
		// clamp future: both ends cannot be after today
		if toDay.After(todayStart) {
			toDay = todayStart
		}
		if from.After(todayStart) {
			// entirely future month → empty but valid single-day window at today
			from = todayStart
			toDay = todayStart
		}
		if from.After(toDay) {
			from = toDay
		}
		return from, toDay, nil
	}
	if q.Days > 0 {
		toDay = todayStart
		from = todayStart.AddDate(0, 0, -(q.Days - 1))
		return from, toDay, nil
	}
	// all time
	toDay = todayStart
	var earliest sql.NullString
	_ = s.db.QueryRow(`SELECT MIN(occurred_at) FROM transactions`).Scan(&earliest)
	if earliest.Valid {
		t, e := time.Parse(time.RFC3339Nano, earliest.String)
		if e != nil {
			t, _ = time.Parse(time.RFC3339, earliest.String)
		}
		if !t.IsZero() {
			lt := t.In(loc)
			from = time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
			return from, toDay, nil
		}
	}
	return todayStart, todayStart, nil
}

func (s *Store) rangeTotals(ctx context.Context, from, toExclusive time.Time, loc *time.Location, kind string) (RangeTotals, error) {
	q := `
SELECT amount, direction, occurred_at
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), toExclusive.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return RangeTotals{}, err
	}
	defer rows.Close()

	var tot RangeTotals
	active := map[string]struct{}{}
	for rows.Next() {
		var amount float64
		var direction, occurred string
		if err := rows.Scan(&amount, &direction, &occurred); err != nil {
			return RangeTotals{}, err
		}
		tot.TxnCount++
		ts, err := parseTimeFlex(occurred)
		if err == nil {
			active[ts.In(loc).Format("2006-01-02")] = struct{}{}
		}
		switch model.Direction(direction) {
		case model.DirectionOut:
			tot.Expense += amount
			tot.ExpenseCount++
		case model.DirectionIn:
			tot.Income += amount
			tot.IncomeCount++
		}
	}
	tot.ActiveDays = len(active)
	tot.Expense = round2(tot.Expense)
	tot.Income = round2(tot.Income)
	tot.Net = round2(tot.Income - tot.Expense)
	if tot.ActiveDays > 0 {
		tot.AvgDailyExpense = round2(tot.Expense / float64(tot.ActiveDays))
	}
	return tot, rows.Err()
}

// DailySummariesFrom builds continuous daily buckets starting at from for nDays.
func (s *Store) DailySummariesFrom(ctx context.Context, from time.Time, nDays int, loc *time.Location, kind string) ([]model.DailySummary, error) {
	if nDays <= 0 {
		nDays = 7
	}
	end := from.AddDate(0, 0, nDays)
	q := `
SELECT amount, direction, occurred_at
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += `
ORDER BY occurred_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDate := make(map[string]*model.DailySummary, nDays)
	order := make([]string, 0, nDays)
	for i := 0; i < nDays; i++ {
		d := from.AddDate(0, 0, i).Format("2006-01-02")
		byDate[d] = &model.DailySummary{Date: d}
		order = append(order, d)
	}

	for rows.Next() {
		var amount float64
		var direction, occurred string
		if err := rows.Scan(&amount, &direction, &occurred); err != nil {
			return nil, err
		}
		ts, err := parseTimeFlex(occurred)
		if err != nil {
			continue
		}
		day := ts.In(loc).Format("2006-01-02")
		sum, ok := byDate[day]
		if !ok {
			continue
		}
		sum.TxnCount++
		switch model.Direction(direction) {
		case model.DirectionOut:
			sum.Expense += amount
		case model.DirectionIn:
			sum.Income += amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]model.DailySummary, 0, len(order))
	for _, d := range order {
		sum := byDate[d]
		sum.Expense = round2(sum.Expense)
		sum.Income = round2(sum.Income)
		sum.Net = round2(sum.Income - sum.Expense)
		out = append(out, *sum)
	}
	return out, nil
}

func (s *Store) monthlyStats(ctx context.Context, from, toExclusive time.Time, loc *time.Location, kind string) ([]MonthlyStat, error) {
	q := `
SELECT amount, direction, occurred_at
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), toExclusive.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += `
ORDER BY occurred_at ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	order := []string{}
	by := map[string]*MonthlyStat{}
	// prefill months from from..to
	cur := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, loc)
	endM := time.Date(toExclusive.Add(-time.Nanosecond).Year(), toExclusive.Add(-time.Nanosecond).Month(), 1, 0, 0, 0, 0, loc)
	for !cur.After(endM) {
		k := cur.Format("2006-01")
		by[k] = &MonthlyStat{Month: k}
		order = append(order, k)
		cur = cur.AddDate(0, 1, 0)
	}

	for rows.Next() {
		var amount float64
		var direction, occurred string
		if err := rows.Scan(&amount, &direction, &occurred); err != nil {
			return nil, err
		}
		ts, err := parseTimeFlex(occurred)
		if err != nil {
			continue
		}
		k := ts.In(loc).Format("2006-01")
		st, ok := by[k]
		if !ok {
			st = &MonthlyStat{Month: k}
			by[k] = st
			order = append(order, k)
		}
		st.TxnCount++
		switch model.Direction(direction) {
		case model.DirectionOut:
			st.Expense += amount
		case model.DirectionIn:
			st.Income += amount
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]MonthlyStat, 0, len(order))
	for _, k := range order {
		st := by[k]
		st.Expense = round2(st.Expense)
		st.Income = round2(st.Income)
		st.Net = round2(st.Income - st.Expense)
		out = append(out, *st)
	}
	return out, nil
}

func (s *Store) channelStats(ctx context.Context, from, toExclusive time.Time, totalExpense float64, kind string) ([]ChannelStat, error) {
	q := `
SELECT COALESCE(NULLIF(merchant_norm, ''), NULLIF(merchant, ''), '未知') AS name,
       SUM(CASE WHEN direction = 'out' THEN amount ELSE 0 END) AS expense,
       SUM(CASE WHEN direction = 'in' THEN amount ELSE 0 END) AS income,
       COUNT(*) AS cnt
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), toExclusive.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += `
GROUP BY name
ORDER BY expense DESC, income DESC
LIMIT 20`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChannelStat
	for rows.Next() {
		var st ChannelStat
		if err := rows.Scan(&st.Name, &st.Expense, &st.Income, &st.TxnCount); err != nil {
			return nil, err
		}
		st.Expense = round2(st.Expense)
		st.Income = round2(st.Income)
		if totalExpense > 0 {
			st.Pct = round2(st.Expense / totalExpense * 100)
		}
		out = append(out, st)
	}
	if out == nil {
		out = []ChannelStat{}
	}
	return out, rows.Err()
}

func (s *Store) categoryStats(ctx context.Context, from, toExclusive time.Time, totalExpense float64, kind string) ([]CategoryStat, error) {
	q := `
SELECT COALESCE(NULLIF(category, ''), '其他') AS name,
       SUM(CASE WHEN direction = 'out' THEN amount ELSE 0 END) AS expense,
       SUM(CASE WHEN direction = 'in' THEN amount ELSE 0 END) AS income,
       COUNT(*) AS cnt
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?`
	args := []any{from.UTC().Format(time.RFC3339Nano), toExclusive.UTC().Format(time.RFC3339Nano)}
	if kind != "" && kind != "all" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	q += `
GROUP BY name
ORDER BY expense DESC, income DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CategoryStat
	for rows.Next() {
		var st CategoryStat
		if err := rows.Scan(&st.Name, &st.Expense, &st.Income, &st.TxnCount); err != nil {
			return nil, err
		}
		st.Expense = round2(st.Expense)
		st.Income = round2(st.Income)
		if totalExpense > 0 {
			st.Pct = round2(st.Expense / totalExpense * 100)
		}
		out = append(out, st)
	}
	if out == nil {
		out = []CategoryStat{}
	}
	return out, rows.Err()
}

func (s *Store) topExpenses(ctx context.Context, from, toExclusive time.Time, limit int) ([]model.Transaction, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, txnSelect+`
WHERE t.occurred_at >= ? AND t.occurred_at < ? AND t.direction = 'out'
ORDER BY t.amount DESC, t.occurred_at DESC
LIMIT ?`,
		from.UTC().Format(time.RFC3339Nano),
		toExclusive.UTC().Format(time.RFC3339Nano),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanTxnList(rows)
	if err != nil {
		return nil, err
	}
	return s.EnrichTransactions(ctx, list)
}

func (s *Store) recentByDirection(ctx context.Context, from, toExclusive time.Time, dir model.Direction, limit int) ([]model.Transaction, error) {
	if limit <= 0 {
		limit = 8
	}
	rows, err := s.db.QueryContext(ctx, txnSelect+`
WHERE t.occurred_at >= ? AND t.occurred_at < ? AND t.direction = ?
ORDER BY t.occurred_at DESC, t.id DESC
LIMIT ?`,
		from.UTC().Format(time.RFC3339Nano),
		toExclusive.UTC().Format(time.RFC3339Nano),
		dir,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanTxnList(rows)
	if err != nil {
		return nil, err
	}
	return s.EnrichTransactions(ctx, list)
}

func (s *Store) balanceSeries(ctx context.Context, from, toExclusive time.Time, limit int) ([]BalancePoint, error) {
	if limit <= 0 {
		limit = 120
	}
	// Take evenly if too many: fetch all in range ordered, then downsample.
	rows, err := s.db.QueryContext(ctx, `
SELECT balance_after, occurred_at
FROM transactions
WHERE balance_known = 1 AND balance_after IS NOT NULL
  AND occurred_at >= ? AND occurred_at < ?
ORDER BY occurred_at ASC`,
		from.UTC().Format(time.RFC3339Nano),
		toExclusive.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []BalancePoint
	for rows.Next() {
		var bal float64
		var occurred string
		if err := rows.Scan(&bal, &occurred); err != nil {
			return nil, err
		}
		ts, err := parseTimeFlex(occurred)
		if err != nil {
			continue
		}
		all = append(all, BalancePoint{At: ts, Balance: bal})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(all) <= limit {
		if all == nil {
			all = []BalancePoint{}
		}
		return all, nil
	}
	// Downsample keeping first/last.
	out := make([]BalancePoint, 0, limit)
	step := float64(len(all)-1) / float64(limit-1)
	for i := 0; i < limit; i++ {
		idx := int(float64(i) * step)
		if idx >= len(all) {
			idx = len(all) - 1
		}
		out = append(out, all[idx])
	}
	return out, nil
}

func scanTxnList(rows *sql.Rows) ([]model.Transaction, error) {
	var out []model.Transaction
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if out == nil {
		out = []model.Transaction{}
	}
	return out, rows.Err()
}

func parseTimeFlex(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func (s *Store) kindStats(ctx context.Context, from, toExclusive time.Time, total float64) ([]ChannelStat, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(kind, ''), 'other') AS name,
       SUM(CASE WHEN direction = 'out' THEN amount ELSE 0 END) AS expense,
       SUM(CASE WHEN direction = 'in' THEN amount ELSE 0 END) AS income,
       COUNT(*) AS cnt
FROM transactions
WHERE occurred_at >= ? AND occurred_at < ?
GROUP BY name
ORDER BY expense DESC, income DESC`,
		from.UTC().Format(time.RFC3339Nano),
		toExclusive.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelStat
	for rows.Next() {
		var st ChannelStat
		var raw string
		if err := rows.Scan(&raw, &st.Expense, &st.Income, &st.TxnCount); err != nil {
			return nil, err
		}
		st.Name = model.KindLabel(model.Kind(raw))
		if st.Name == "其他" && raw != "" && raw != "other" {
			st.Name = raw
		}
		st.Expense = round2(st.Expense)
		st.Income = round2(st.Income)
		if total > 0 {
			st.Pct = round2((st.Expense + st.Income) / total * 100)
		}
		out = append(out, st)
	}
	if out == nil {
		out = []ChannelStat{}
	}
	return out, rows.Err()
}

func (s *Store) computeBalanceHealth(series []BalancePoint, avgDailyExpense, lowThreshold float64) BalanceHealth {
	h := BalanceHealth{LowThreshold: lowThreshold}
	if len(series) == 0 {
		return h
	}
	sum := 0.0
	h.MinBalance = series[0].Balance
	h.MaxBalance = series[0].Balance
	var lastLow *time.Time
	for i := range series {
		b := series[i].Balance
		sum += b
		if b < h.MinBalance {
			h.MinBalance = b
		}
		if b > h.MaxBalance {
			h.MaxBalance = b
		}
		if b <= lowThreshold {
			h.LowHitCount++
			at := series[i].At
			lastLow = &at
		}
	}
	h.SampleCount = len(series)
	h.AvgBalance = round2(sum / float64(len(series)))
	h.MinBalance = round2(h.MinBalance)
	h.MaxBalance = round2(h.MaxBalance)
	h.LastLowAt = lastLow
	latest := series[len(series)-1].Balance
	if avgDailyExpense > 0 {
		h.DaysOfRunway = round2(latest / avgDailyExpense)
	}
	return h
}
