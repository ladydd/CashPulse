package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"
)

func cst() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

func TestPSBC_ExpenseWeChat(t *testing.T) {
	p := New(cst())
	text := "【邮储银行】26年07月17日12:22您尾号9653账户快捷支付-微信支付（财付通），支出金额17.00元，余额1324.69元"
	res, err := p.Parse(text, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	txn := res.Transaction
	if txn.Amount != 17 || txn.Direction != "out" {
		t.Fatalf("amount/dir = %v %v", txn.Amount, txn.Direction)
	}
	if txn.Merchant != "微信支付" || txn.Category != "消费" {
		t.Fatalf("merchant/cat = %q %q", txn.Merchant, txn.Category)
	}
	if !txn.BalanceKnown || txn.BalanceAfter != 1324.69 {
		t.Fatalf("balance = known=%v %v", txn.BalanceKnown, txn.BalanceAfter)
	}
	if txn.CardLast4 != "9653" {
		t.Fatalf("card = %q", txn.CardLast4)
	}
	if txn.OccurredAt.Year() != 2026 || txn.OccurredAt.Month() != 7 || txn.OccurredAt.Day() != 17 {
		t.Fatalf("time = %v", txn.OccurredAt)
	}
	if res.MatchedRule != "psbc" {
		t.Fatalf("rule = %s", res.MatchedRule)
	}
}

func TestPSBC_IncomeInbound(t *testing.T) {
	p := New(cst())
	text := "【邮储银行】26年06月15日15:04某公司账户9511向您尾号9653账户他行汇入，收入金额8407.27元，余额8419.67元"
	res, err := p.Parse(text, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	txn := res.Transaction
	if txn.Direction != "in" || txn.Amount != 8407.27 {
		t.Fatalf("got dir=%s amt=%v", txn.Direction, txn.Amount)
	}
	if txn.Category != "转账收入" {
		t.Fatalf("category = %q", txn.Category)
	}
	if txn.Merchant == "" {
		t.Fatal("merchant empty")
	}
}

func TestPSBC_Outbound(t *testing.T) {
	p := New(cst())
	text := "【邮储银行】26年06月15日20:35您尾号9653账户向崔某尾号2863账户跨行汇出，支出金额8000.00元，余额387.77元"
	res, err := p.Parse(text, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Transaction.Direction != "out" || res.Transaction.Category != "转账" {
		t.Fatalf("%+v", res.Transaction)
	}
}

func TestPSBC_Refund(t *testing.T) {
	p := New(cst())
	text := "【邮储银行】25年04月09日16:09您尾号653账户退货，收入金额39.80元，余额233.73元"
	res, err := p.Parse(text, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Transaction.Direction != "in" || res.Transaction.Category != "退款" {
		t.Fatalf("%+v", res.Transaction)
	}
}

func TestPSBC_OTP_Ignored(t *testing.T) {
	p := New(cst())
	text := "【邮储银行】验证码：598659（序号5378），您向崔*尾号2863账户转账8000.00元。任何人索要验证码均为诈骗，请勿泄露！"
	res, err := p.Parse(text, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ignored || res.IgnoreReason != "otp" {
		t.Fatalf("expected ignored otp, got %+v", res)
	}
	if res.Transaction != nil {
		t.Fatal("otp should not produce transaction")
	}
}

func TestPSBC_SMSFee(t *testing.T) {
	p := New(cst())
	text := "【邮储银行】26年04月01日19:48您尾号9653账户扣除2026年04月短信费，支出金额3.00元，余额91.78元"
	res, err := p.Parse(text, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Transaction.Merchant != "短信费" || res.Transaction.Category != "手续费" {
		t.Fatalf("%+v", res.Transaction)
	}
}

func TestPSBC_AllExportMessages(t *testing.T) {
	// Full export regression: every non-header line should parse or be ignored.
	_, thisFile, _, _ := runtime.Caller(0)
	// internal/parser -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
	path := filepath.Join(root, "tmp", "95580短信完整导出.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("export file not present:", err)
	}

	reLine := regexp.MustCompile(`(?m)^\d{3}\s+(【.+)$`)
	matches := reLine.FindAllStringSubmatch(string(data), -1)
	if len(matches) < 300 {
		t.Fatalf("expected ~308 msgs, got %d", len(matches))
	}

	p := New(cst())
	var ok, ignored, failed int
	for _, m := range matches {
		text := m[1]
		res, err := p.Parse(text, time.Now())
		if err != nil {
			failed++
			t.Errorf("FAIL: %s (%v)", text, err)
			continue
		}
		if res.Ignored {
			ignored++
			continue
		}
		if res.Transaction == nil {
			failed++
			t.Errorf("nil txn: %s", text)
			continue
		}
		if res.Transaction.Amount < 0 {
			t.Errorf("negative amount: %s", text)
		}
		if !res.Transaction.BalanceKnown {
			t.Errorf("missing balance: %s", text)
		}
		if res.Transaction.OccurredAt.IsZero() {
			t.Errorf("zero time: %s", text)
		}
		ok++
	}
	t.Logf("parsed=%d ignored=%d failed=%d total=%d", ok, ignored, failed, len(matches))
	if failed > 0 {
		t.Fatalf("%d messages failed to parse", failed)
	}
	if ignored != 2 {
		t.Fatalf("expected 2 OTP ignored, got %d", ignored)
	}
	if ok != 306 {
		t.Fatalf("expected 306 transactions, got %d", ok)
	}
}
