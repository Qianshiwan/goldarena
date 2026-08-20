package payment

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
)

// Provider abstracts a real-money payment gateway. The aggregator implementation
// (易支付/码付 style) and WeChat Pay v3 Native are wired today; adding Alipay
// direct or Stripe means adding another Provider here without touching handlers.
type Provider interface {
	Name() string
	CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error)
	// VerifyNotify validates an async callback from the gateway and returns the
	// local out_trade_no on success.
	//   - rawBody: the raw HTTP request body bytes
	//   - headers: HTTP request headers (used by wechatpay for signature verification)
	//   - key: merchant secret / API key used for signing
	VerifyNotify(rawBody []byte, headers http.Header, key string) (outTradeNo string, ok bool, err error)
}

// CreateOrderInput carries everything needed to open a charge at the gateway.
type CreateOrderInput struct {
	Gateway    string  // gateway submit URL
	PID        string  // merchant id
	Key        string  // merchant secret (signing)
	NotifyURL  string  // async callback the gateway calls when paid
	ReturnURL  string  // where the user is sent after paying
	OutTradeNo string  // our local unique order id
	Channel    string  // wxpay | alipay
	Amount     float64 // RMB
	Name       string  // product name shown to payer
}

// CreateOrderResult is what the gateway returns to render to the user.
type CreateOrderResult struct {
	QRContent string // scannable pay code (e.g. weixin://wxpay/...)
	PayURL    string // fallback browser pay URL
	TradeNo   string // gateway's own trade number
}

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
