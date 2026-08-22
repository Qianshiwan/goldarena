package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/errs"
	"github.com/goldarena/goldarena/pkg/payment"
	qrcode "github.com/skip2/go-qrcode"
)

// PaymentService handles real-money recharge: create a payment order, generate a
// scannable QR, and credit the wallet only after the provider confirms payment
// (via async notify, or a local simulate call in sandbox mode).
type PaymentService struct {
	mem     *common.MemoryStore
	provider payment.Provider
	sandbox bool
	rate    float64 // RMB -> game coins (¥1 = rate coins)
	baseURL string // public base URL for notify/return callbacks
	gateway string
	pid     string
	key     string
}

func NewPaymentService(mem *common.MemoryStore, provider payment.Provider, sandbox bool, rate float64, baseURL, gateway, pid, key string) *PaymentService {
	return &PaymentService{mem: mem, provider: provider, sandbox: sandbox, rate: rate, baseURL: baseURL, gateway: gateway, pid: pid, key: key}
}

type paymentCreateReq struct {
	Amount  float64 `json:"amount" binding:"required,min=10"`
	Channel string  `json:"channel"` // wxpay | alipay
}

// CreateOrder opens a charge. In sandbox mode it returns a local simulate URL so
// the whole UX (QR -> scan/pay -> credit) can be tested without real credentials.
func (s *PaymentService) CreateOrder(c *gin.Context) {
	userID := c.GetInt64("user_id")
	var req paymentCreateReq
	if err := common.BindJSON(c, &req); err != nil {
		return
	}
	channel := req.Channel
	if channel == "" {
		channel = "wxpay"
	}
	if channel != "wxpay" && channel != "alipay" {
		common.Error(c, errs.InvalidParam, "channel must be wxpay or alipay")
		return
	}

	gameCoins := req.Amount * s.rate
	outTradeNo := fmt.Sprintf("GA%d%06d%d", time.Now().UnixNano()/1e6, userID, time.Now().Nanosecond()%100000)
	order := &common.PaymentOrder{
		UserID:     userID,
		OutTradeNo: outTradeNo,
		Channel:    channel,
		AmountRMB:  req.Amount,
		GameCoins:  gameCoins,
		Status:     common.PaymentPending,
		Provider:   "aggregator",
		CreatedAt:  time.Now(),
	}

	if s.sandbox {
		order.Provider = "sandbox"
		order.QRContent = outTradeNo
		order.PayURL = fmt.Sprintf("%s/api/v1/payment/simulate?out_trade_no=%s", s.baseURL, outTradeNo)
		s.mem.SavePaymentOrder(order)
		common.Success(c, gin.H{
			"sandbox":    true,
			"order":      order,
			"qr_content": order.QRContent,
			"pay_url":    order.PayURL,
		})
		return
	}

	if s.provider == nil {
		common.Error(c, errs.Internal, "payment provider not configured")
		return
	}
	in := payment.CreateOrderInput{
		Gateway:    s.gateway,
		PID:        s.pid,
		Key:        s.key,
		NotifyURL:  s.baseURL + "/api/v1/payment/notify",
		ReturnURL:  s.baseURL + "/api/v1/payment/simulate?out_trade_no=" + outTradeNo,
		OutTradeNo: outTradeNo,
		Channel:    channel,
		Amount:     req.Amount,
		Name:       "金归子游戏币",
	}
	res, err := s.provider.CreateOrder(context.Background(), in)
	if err != nil {
		common.Error(c, errs.Internal, "create payment failed: "+err.Error())
		return
	}
	order.QRContent = res.QRContent
	order.PayURL = res.PayURL
	s.mem.SavePaymentOrder(order)
	common.Success(c, gin.H{
		"sandbox":    false,
		"order":      order,
		"qr_content": res.QRContent,
		"pay_url":    res.PayURL,
	})
}

// Notify is the public async callback from the payment gateway. It verifies the
// signature and credits the wallet. The gateway expects the literal "success".
//   - Aggregator (易支付): posts form-encoded body; we pass raw body bytes
//   - WeChat Pay v3: posts JSON with AES-256-GCM encrypted body + signature headers
func (s *PaymentService) Notify(c *gin.Context) {
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.String(200, "fail")
		return
	}
	if s.sandbox {
		c.String(200, "success")
		return
	}
	if s.provider == nil {
		c.String(200, "fail")
		return
	}
	outTradeNo, ok, err := s.provider.VerifyNotify(rawBody, c.Request.Header, s.key)
	if err != nil || !ok {
		// Different response formats per provider:
		//   WeChat Pay v3: JSON {code:"FAIL"}
		//   WeChat Pay v2: XML <return_code>FAIL</return_code>
		//   Aggregator: plain text "fail"
		switch s.provider.Name() {
		case "wechatpay":
			c.JSON(200, gin.H{"code": "FAIL", "message": fmt.Sprintf("verify: %v", err)})
		case "wechatpay_v2":
			c.Data(200, "application/xml", []byte(fmt.Sprintf(`<xml><return_code>FAIL</return_code><return_msg>%s</return_msg></xml>`, err.Error())))
		default:
			c.String(200, "fail")
		}
		return
	}
	s.creditIfPending(outTradeNo)
	// Success response per provider:
	//   WeChat Pay v3: JSON {code:"SUCCESS"}
	//   WeChat Pay v2: XML <return_code>SUCCESS</return_code>
	//   Aggregator: plain text "success"
	switch s.provider.Name() {
	case "wechatpay":
		c.JSON(200, gin.H{"code": "SUCCESS"})
	case "wechatpay_v2":
		c.Data(200, "application/xml", []byte(`<xml><return_code>SUCCESS</return_code></xml>`))
	default:
		c.String(200, "success")
	}
}

// SimulatePaid is a sandbox-only endpoint that mimics the gateway's successful
// callback so the recharge flow can be exercised locally. Disabled in production.
func (s *PaymentService) SimulatePaid(c *gin.Context) {
	if !s.sandbox {
		common.Error(c, errs.Forbidden, "simulate only allowed in sandbox mode")
		return
	}
	var req struct {
		OutTradeNo string `json:"out_trade_no"`
	}
	_ = common.BindJSON(c, &req)
	if req.OutTradeNo == "" {
		req.OutTradeNo = c.Query("out_trade_no")
	}
	if req.OutTradeNo == "" {
		common.Error(c, errs.InvalidParam, "out_trade_no required")
		return
	}
	s.creditIfPending(req.OutTradeNo)
	common.Success(c, gin.H{"status": common.PaymentPaid})
}

// ListOrders returns the current user's payment orders (newest first).
func (s *PaymentService) ListOrders(c *gin.Context) {
	userID := c.GetInt64("user_id")
	common.Success(c, s.mem.GetPaymentOrdersByUser(userID))
}

// QRCode returns a PNG QR encoding the given text (the pay code / pay URL).
func (s *PaymentService) QRCode(c *gin.Context) {
	text := c.Query("text")
	if text == "" {
		common.Error(c, errs.InvalidParam, "text required")
		return
	}
	png, err := qrcode.Encode(text, qrcode.Medium, 256)
	if err != nil {
		common.Error(c, errs.Internal, "qr generate failed")
		return
	}
	c.Data(200, "image/png", png)
}

// creditIfPending credits the wallet exactly once for a pending order.
func (s *PaymentService) creditIfPending(outTradeNo string) {
	order := s.mem.GetPaymentOrderByOutTradeNo(outTradeNo)
	if order == nil || order.Status != common.PaymentPending {
		return // already paid / missing — idempotent
	}
	wallet := s.mem.GetWallet(order.UserID)
	if wallet == nil {
		return
	}
	balanceBefore := wallet.Balance
	newBalance := balanceBefore + order.GameCoins
	s.mem.UpdateWalletBalance(order.UserID, newBalance, wallet.Frozen)
	s.mem.SaveWalletTransaction(order.UserID, &common.WalletTransaction{
		ID:            time.Now().UnixNano(),
		UserID:        order.UserID,
		Type:          "recharge",
		Amount:        order.GameCoins,
		BalanceBefore: balanceBefore,
		BalanceAfter:  newBalance,
		Remark:        fmt.Sprintf("支付(%s)充值%.0f元→%.0f游戏币", order.Channel, order.AmountRMB, order.GameCoins),
		CreatedAt:     time.Now(),
	})
	now := time.Now()
	s.mem.UpdatePaymentOrderStatus(outTradeNo, common.PaymentPaid, &now)
}
