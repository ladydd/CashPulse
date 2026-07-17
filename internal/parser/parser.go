package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cashpulse/internal/classify"
	"cashpulse/internal/model"
)

// Result is the outcome of parsing one SMS.
type Result struct {
	Transaction  *model.Transaction
	MatchedRule  string
	Ignored      bool
	IgnoreReason string
}

// Parser turns raw bank SMS text into structured transactions.
// Rules are tried in order; first match wins.
type Parser struct {
	rules []rule
	loc   *time.Location
}

type rule struct {
	name string
	// ok=true means this rule handled the text (either as txn or as ignore).
	fn func(text string, now time.Time, loc *time.Location) (Result, bool)
}

// New returns a parser with built-in rules.
func New(loc *time.Location) *Parser {
	if loc == nil {
		loc = time.Local
	}
	p := &Parser{loc: loc}
	p.rules = []rule{
		{name: "psbc", fn: parsePSBC},           // 邮储银行（主卡）
		{name: "generic_cn_bank", fn: parseGenericCN},
	}
	return p
}

// Parse attempts to extract a transaction from SMS text.
func (p *Parser) Parse(text string, now time.Time) (Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Result{}, fmt.Errorf("empty sms text")
	}
	if now.IsZero() {
		now = time.Now()
	}

	for _, r := range p.rules {
		res, ok := r.fn(text, now, p.loc)
		if !ok {
			continue
		}
		if res.MatchedRule == "" {
			res.MatchedRule = r.name
		}
		return res, nil
	}
	return Result{}, fmt.Errorf("no parser rule matched")
}

// ---- 邮储银行（95580） -------------------------------------------------------

var (
	rePSBCHeader = regexp.MustCompile(`【邮储银行】`)
	// 24年11月23日13:21  /  25年07月09日10:48
	rePSBCTime = regexp.MustCompile(`(\d{2})年(\d{1,2})月(\d{1,2})日\s*(\d{1,2}):(\d{2})`)
	// 您尾号653 / 您尾号9653
	rePSBCMyCard = regexp.MustCompile(`您尾号(\d{3,6})`)
	// 支出金额11.16元 / 收入金额100.00元
	rePSBCAmount = regexp.MustCompile(`(支出|收入)金额(\d+(?:\.\d{1,2})?)元`)
	// 余额915.70元（句末可能无句号）
	rePSBCBalance = regexp.MustCompile(`余额(\d+(?:\.\d{1,2})?)元`)
	// 账户后、金额前的渠道描述
	// e.g. 账户快捷支付-微信支付（财付通），支出金额
	// e.g. 账户向张亚玲尾号5409账户跨行汇出，支出金额
	// e.g. 张亚玲账户5409向您尾号653账户他行汇入，收入金额
	rePSBCChannel = regexp.MustCompile(`您尾号\d{3,6}账户([^，,]+)，(?:支出|收入)金额`)
	// 对方 → 我：NAME账户NNNN向您尾号...他行汇入
	rePSBCInboundFrom = regexp.MustCompile(`([^【】\d]{1,40}?)账户(\d{3,6})向您尾号\d{3,6}账户([^，,]*)，收入金额`)
	// 我 → 对方：向NAME尾号NNNN账户跨行汇出
	rePSBCOutboundTo = regexp.MustCompile(`向([^尾号]{1,30}?)尾号(\d{3,6})账户`)
)

func parsePSBC(text string, now time.Time, loc *time.Location) (Result, bool) {
	if !rePSBCHeader.MatchString(text) {
		return Result{}, false
	}

	// 验证码：不是流水
	if strings.Contains(text, "验证码") {
		return Result{
			Ignored:      true,
			IgnoreReason: "otp",
			MatchedRule:  "psbc",
		}, true
	}

	am := rePSBCAmount.FindStringSubmatch(text)
	if am == nil {
		return Result{}, false
	}
	amount, err := strconv.ParseFloat(am[2], 64)
	if err != nil {
		return Result{}, false
	}
	direction := model.DirectionOut
	if am[1] == "收入" {
		direction = model.DirectionIn
	}

	txn := &model.Transaction{
		Amount:     amount,
		Currency:   "CNY",
		Direction:  direction,
		CardLast4:  firstSub(rePSBCMyCard, text, 1),
		OccurredAt: parsePSBCTime(text, now, loc),
		Bank:       "邮储银行",
	}

	if bm := rePSBCBalance.FindStringSubmatch(text); bm != nil {
		if bal, err := strconv.ParseFloat(bm[1], 64); err == nil {
			txn.BalanceAfter = bal
			txn.BalanceKnown = true
		}
	}

	merchant, category, note := classifyPSBC(text)
	txn.Merchant = merchant
	txn.Category = category
	txn.Note = note
	classify.Enrich(txn)

	return Result{Transaction: txn, MatchedRule: "psbc"}, true
}

func parsePSBCTime(text string, now time.Time, loc *time.Location) time.Time {
	m := rePSBCTime.FindStringSubmatch(text)
	if m == nil {
		return now
	}
	yy, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	hour, _ := strconv.Atoi(m[4])
	min, _ := strconv.Atoi(m[5])
	// 两位年份：00-69 → 2000+，70-99 → 1900+（本项目全是 20xx）
	year := 2000 + yy
	if yy >= 70 {
		year = 1900 + yy
	}
	return time.Date(year, time.Month(month), day, hour, min, 0, 0, loc)
}

func classifyPSBC(text string) (merchant, category, note string) {
	// 入账：对方 → 我
	if m := rePSBCInboundFrom.FindStringSubmatch(text); m != nil {
		name := strings.TrimSpace(m[1])
		// 去掉时间前缀残留（理论上不会）
		name = strings.TrimLeft(name, "0123456789年月日: ")
		channel := strings.TrimSpace(m[3])
		if channel == "" {
			channel = "他行汇入"
		}
		merchant = name
		if merchant == "" {
			merchant = channel
		}
		category = "转账收入"
		note = channel
		if m[2] != "" {
			note = fmt.Sprintf("%s 对方尾号%s", channel, m[2])
		}
		return
	}

	// 渠道串
	ch := ""
	if m := rePSBCChannel.FindStringSubmatch(text); m != nil {
		ch = strings.TrimSpace(m[1])
	}

	// 出账：我 → 对方
	if m := rePSBCOutboundTo.FindStringSubmatch(text); m != nil {
		merchant = strings.TrimSpace(m[1])
		category = "转账"
		note = fmt.Sprintf("跨行汇出 对方尾号%s", m[2])
		if ch != "" && merchant == "" {
			merchant = ch
		}
		return
	}

	switch {
	case strings.Contains(ch, "微信支付") || strings.Contains(ch, "财付通"):
		merchant, category = "微信支付", "消费"
	case strings.Contains(ch, "支付宝"):
		merchant, category = "支付宝", "消费"
	case strings.Contains(ch, "拼多多"):
		merchant, category = "拼多多", "消费"
	case strings.Contains(ch, "抖音"):
		merchant, category = "抖音", "消费"
	case strings.Contains(ch, "京东"):
		merchant, category = "京东", "消费"
	case strings.Contains(ch, "微信红包"):
		merchant, category = "微信红包", "转账"
	case strings.Contains(ch, "微信转账"):
		merchant, category = "微信转账", "转账"
	case strings.Contains(ch, "投资理财") || strings.Contains(ch, "代中间业") || strings.Contains(ch, "理财"):
		merchant, category = "投资理财", "理财"
	case strings.Contains(ch, "退货"):
		merchant, category = "退货", "退款"
	case strings.Contains(ch, "跨行退款"):
		merchant, category = "跨行退款", "退款"
	case strings.Contains(ch, "短信费"):
		merchant, category = "短信费", "手续费"
	case strings.Contains(ch, "网联入账"):
		merchant, category = "网联入账", "入账"
	case strings.Contains(ch, "银联入账"):
		merchant, category = "银联入账", "入账"
	case strings.Contains(ch, "银联快捷"):
		merchant, category = "银联快捷", "消费"
	case strings.Contains(ch, "快捷支付"):
		merchant, category = "快捷支付", "消费"
	case ch == "消费":
		// "账户消费" — POS/debit without channel brand
		merchant, category = "刷卡消费", "消费"
	case strings.Contains(ch, "付款"):
		merchant, category = "付款入账", "入账"
	case ch != "":
		merchant, category = ch, "其他"
	default:
		merchant, category = "未知", "其他"
	}
	note = ch
	return
}

// ---- generic fallback -------------------------------------------------------

var (
	reAmountOut = regexp.MustCompile(`(?i)(?:消费|支出|支付|刷卡|取出|转出|扣款)[^\d]{0,12}(?:RMB|CNY|￥|¥)?\s*(\d+(?:\.\d{1,2})?)\s*元?`)
	reAmountIn  = regexp.MustCompile(`(?i)(?:收入|入账|存入|转入|收款|退款|到账)[^\d]{0,12}(?:RMB|CNY|￥|¥)?\s*(\d+(?:\.\d{1,2})?)\s*元?`)
	reAmountAny = regexp.MustCompile(`(?i)(?:RMB|CNY|￥|¥)\s*(\d+(?:\.\d{1,2})?)|(\d+(?:\.\d{1,2})?)\s*元`)
	reCardLast4 = regexp.MustCompile(`(?:尾号|卡号后四位|卡\*+)(\d{3,6})`)
	reMerchant  = regexp.MustCompile(`(?:在|向|商户)([^\s，,。；;]{2,40}?)(?:完成|交易|消费|支付|成功|$)`)
	reTimeCN    = regexp.MustCompile(`(\d{1,2})月(\d{1,2})日\s*(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	reTimeDash  = regexp.MustCompile(`(?:(\d{4})[-/])?(\d{1,2})[-/](\d{1,2})\s+(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	reBalance   = regexp.MustCompile(`余额[为:]?\s*(?:RMB|CNY|￥|¥)?\s*(\d+(?:\.\d{1,2})?)\s*元?`)
)

func parseGenericCN(text string, now time.Time, loc *time.Location) (Result, bool) {
	// OTP 通用忽略
	if strings.Contains(text, "验证码") {
		return Result{Ignored: true, IgnoreReason: "otp", MatchedRule: "generic_cn_bank"}, true
	}

	direction := model.DirectionOut
	amount, ok := firstAmount(reAmountOut, text)
	if !ok {
		amount, ok = firstAmount(reAmountIn, text)
		if ok {
			direction = model.DirectionIn
		}
	}
	if !ok {
		amount, ok = firstAmount(reAmountAny, text)
		if !ok {
			return Result{}, false
		}
		if strings.Contains(text, "收入") || strings.Contains(text, "入账") ||
			strings.Contains(text, "转入") || strings.Contains(text, "到账") ||
			strings.Contains(text, "退款") {
			direction = model.DirectionIn
		}
	}

	txn := &model.Transaction{
		Amount:     amount,
		Currency:   "CNY",
		Direction:  direction,
		Merchant:   firstSub(reMerchant, text, 1),
		CardLast4:  firstSub(reCardLast4, text, 1),
		OccurredAt: parseTime(text, now, loc),
		Bank:       guessBank(text),
	}
	if bm := reBalance.FindStringSubmatch(text); bm != nil {
		if bal, err := strconv.ParseFloat(bm[1], 64); err == nil {
			txn.BalanceAfter = bal
			txn.BalanceKnown = true
		}
	}
	classify.Enrich(txn)
	return Result{Transaction: txn, MatchedRule: "generic_cn_bank"}, true
}

func firstAmount(re *regexp.Regexp, text string) (float64, bool) {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	for i := 1; i < len(m); i++ {
		if m[i] == "" {
			continue
		}
		v, err := strconv.ParseFloat(m[i], 64)
		if err == nil && v >= 0 {
			return v, true
		}
	}
	return 0, false
}

func firstSub(re *regexp.Regexp, text string, group int) string {
	m := re.FindStringSubmatch(text)
	if m == nil || group >= len(m) {
		return ""
	}
	return strings.TrimSpace(m[group])
}

func parseTime(text string, now time.Time, loc *time.Location) time.Time {
	// Prefer PSBC-style 2-digit year if present
	if t := parsePSBCTime(text, now, loc); rePSBCTime.MatchString(text) {
		return t
	}
	if m := reTimeCN.FindStringSubmatch(text); m != nil {
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		hour, _ := strconv.Atoi(m[3])
		min, _ := strconv.Atoi(m[4])
		sec := 0
		if m[5] != "" {
			sec, _ = strconv.Atoi(m[5])
		}
		year := now.In(loc).Year()
		return time.Date(year, time.Month(month), day, hour, min, sec, 0, loc)
	}
	if m := reTimeDash.FindStringSubmatch(text); m != nil {
		year := now.In(loc).Year()
		if m[1] != "" {
			year, _ = strconv.Atoi(m[1])
		}
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		hour, _ := strconv.Atoi(m[4])
		min, _ := strconv.Atoi(m[5])
		sec := 0
		if m[6] != "" {
			sec, _ = strconv.Atoi(m[6])
		}
		return time.Date(year, time.Month(month), day, hour, min, sec, 0, loc)
	}
	return now
}

func guessBank(text string) string {
	banks := []struct{ key, name string }{
		{"邮储银行", "邮储银行"},
		{"工商银行", "工商银行"},
		{"建设银行", "建设银行"},
		{"农业银行", "农业银行"},
		{"中国银行", "中国银行"},
		{"招商银行", "招商银行"},
		{"交通银行", "交通银行"},
		{"浦发银行", "浦发银行"},
		{"中信银行", "中信银行"},
		{"光大银行", "光大银行"},
		{"民生银行", "民生银行"},
		{"兴业银行", "兴业银行"},
		{"平安银行", "平安银行"},
	}
	for _, b := range banks {
		if strings.Contains(text, b.key) {
			return b.name
		}
	}
	return ""
}
