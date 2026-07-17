package classify

import (
	"testing"

	"cashpulse/internal/model"
)

func TestNormalizeMerchant(t *testing.T) {
	cases := []struct {
		m, n, c, want string
	}{
		{"快捷支付-微信支付（财付通）", "", "消费", "微信支付"},
		{"微信支付", "", "消费", "微信支付"},
		{"支付宝", "", "消费", "支付宝"},
		{"拼多多", "", "消费", "拼多多"},
		{"微信转账", "", "转账", "微信转账"},
		{"短信费", "扣除短信费", "手续费", "短信费"},
		{"投资理财-代中间业", "", "理财", "投资理财"},
		{"微信红包", "", "转账", "微信红包"},
		{"消费", "", "消费", "刷卡消费"},
		{"张三", "跨行汇出 对方尾号1234", "转账", "张三"},
	}
	for _, tc := range cases {
		got := NormalizeMerchant(tc.m, tc.n, tc.c)
		if got != tc.want {
			t.Errorf("NormalizeMerchant(%q)=%q want %q", tc.m, got, tc.want)
		}
	}
}

func TestInferKind(t *testing.T) {
	cases := []struct {
		dir                model.Direction
		cat, mer, note, mn string
		want               model.Kind
	}{
		{model.DirectionOut, "消费", "微信支付", "", "微信支付", model.KindConsume},
		{model.DirectionOut, "转账", "卫少东", "跨行汇出", "卫少东", model.KindTransfer},
		{model.DirectionIn, "转账收入", "张三", "他行汇入", "张三", model.KindTransfer},
		{model.DirectionIn, "退款", "退货", "", "退货", model.KindRefund},
		{model.DirectionOut, "手续费", "短信费", "", "短信费", model.KindFee},
		{model.DirectionIn, "入账", "网联入账", "", "网联入账", model.KindIncome},
		{model.DirectionOut, "理财", "投资理财", "代中间业", "投资理财", model.KindInvest},
		{model.DirectionOut, "转账", "微信红包", "", "微信红包", model.KindTransfer},
		{model.DirectionOut, "消费", "消费", "", "刷卡消费", model.KindConsume},
	}
	for _, tc := range cases {
		got := InferKind(tc.dir, tc.cat, tc.mer, tc.note, tc.mn)
		if got != tc.want {
			t.Errorf("InferKind(%v,%q)=%q want %q", tc.dir, tc.cat, got, tc.want)
		}
	}
}

func TestEnrich(t *testing.T) {
	txn := &model.Transaction{
		Direction: model.DirectionOut,
		Merchant:  "快捷支付-微信支付（财付通）",
		Category:  "消费",
	}
	Enrich(txn)
	if txn.MerchantNorm != "微信支付" {
		t.Fatalf("norm=%q", txn.MerchantNorm)
	}
	if txn.Kind != model.KindConsume {
		t.Fatalf("kind=%q", txn.Kind)
	}
}
