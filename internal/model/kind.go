package model

// Kind is the economic nature of a transaction (cleaner than free-text category).
type Kind string

const (
	KindConsume  Kind = "consume"  // 日常消费
	KindTransfer Kind = "transfer" // 转账/红包
	KindRefund   Kind = "refund"   // 退货/退款
	KindFee      Kind = "fee"      // 手续费/短信费
	KindIncome   Kind = "income"   // 入账
	KindInvest   Kind = "invest"   // 理财/投资
	KindOther    Kind = "other"
)

// KindLabel is Chinese UI text.
func KindLabel(k Kind) string {
	switch k {
	case KindConsume:
		return "消费"
	case KindTransfer:
		return "转账"
	case KindRefund:
		return "退款"
	case KindFee:
		return "手续费"
	case KindIncome:
		return "入账"
	case KindInvest:
		return "理财"
	default:
		return "其他"
	}
}
