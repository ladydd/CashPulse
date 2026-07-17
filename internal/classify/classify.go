package classify

import (
	"strings"

	"cashpulse/internal/model"
)

// Enrich fills Kind and MerchantNorm on a parsed transaction (in place).
func Enrich(t *model.Transaction) {
	if t == nil {
		return
	}
	t.MerchantNorm = NormalizeMerchant(t.Merchant, t.Note, t.Category)
	t.Kind = InferKind(t.Direction, t.Category, t.Merchant, t.Note, t.MerchantNorm)
	if t.Category == "" || t.Category == "其他" {
		t.Category = model.KindLabel(t.Kind)
	}
}

// NormalizeMerchant maps raw channel strings to stable display names.
func NormalizeMerchant(merchant, note, category string) string {
	raw := merchant + " " + note + " " + category
	s := strings.TrimSpace(merchant)
	switch {
	case containsAny(raw, "微信支付", "财付通"):
		return "微信支付"
	case containsAny(raw, "支付宝"):
		return "支付宝"
	case containsAny(raw, "拼多多"):
		return "拼多多"
	case containsAny(raw, "抖音"):
		return "抖音"
	case containsAny(raw, "京东", "网银在线"):
		return "京东"
	case containsAny(raw, "微信红包"):
		return "微信红包"
	case containsAny(raw, "微信转账"):
		return "微信转账"
	case containsAny(raw, "投资理财", "代中间业", "理财"):
		return "投资理财"
	case containsAny(raw, "短信费"):
		return "短信费"
	case containsAny(raw, "跨行退款"):
		return "跨行退款"
	case containsAny(raw, "退货"):
		return "退货"
	case containsAny(raw, "银联快捷"):
		return "银联快捷"
	case containsAny(raw, "网联入账"):
		return "网联入账"
	case containsAny(raw, "银联入账"):
		return "银联入账"
	case containsAny(raw, "快捷支付"):
		return "快捷支付"
	// bare "消费" from "账户消费" — POS / bank debit without channel detail
	case s == "消费" || strings.TrimSpace(raw) == "消费 消费":
		return "刷卡消费"
	case containsAny(raw, "他行汇入", "跨行汇出"):
		if s != "" && s != "快捷支付" && !strings.Contains(s, "汇") && s != "消费" {
			return s
		}
		if containsAny(raw, "他行汇入") {
			return "他行汇入"
		}
		return "跨行汇出"
	case s != "":
		return s
	default:
		return "未知"
	}
}

// InferKind decides economic kind from direction + category + text hints.
func InferKind(dir model.Direction, category, merchant, note, merchantNorm string) model.Kind {
	raw := category + " " + merchant + " " + note + " " + merchantNorm
	switch {
	case containsAny(raw, "短信费", "手续费"):
		return model.KindFee
	case containsAny(raw, "退货", "退款"):
		return model.KindRefund
	case containsAny(raw, "投资理财", "代中间业", "理财"):
		return model.KindInvest
	case containsAny(raw, "微信红包", "微信转账", "跨行汇出", "他行汇入", "转账收入") || category == "转账":
		return model.KindTransfer
	case category == "入账" || containsAny(raw, "网联入账", "银联入账", "付款入账"):
		return model.KindIncome
	case category == "消费" || containsAny(raw, "微信支付", "支付宝", "拼多多", "抖音", "京东", "快捷支付", "银联快捷", "刷卡消费"):
		return model.KindConsume
	case dir == model.DirectionIn:
		if containsAny(raw, "汇入", "转账") {
			return model.KindTransfer
		}
		return model.KindIncome
	case dir == model.DirectionOut:
		if containsAny(raw, "汇出", "转账", "红包") {
			return model.KindTransfer
		}
		return model.KindConsume
	default:
		return model.KindOther
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
