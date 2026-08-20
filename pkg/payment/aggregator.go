package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AggregatorProvider implements the common "易支付 / 码付" style gateway used by
// many Chinese payment aggregators. It lets you accept WeChat Pay / Alipay as
// channels WITHOUT owning your own 微信商户号 — the aggregator mints the
// WeChat/Alipay QR for you. Submit builds a signed form POST; the async notify is
// verified before crediting the wallet.
//
// ⚠️ The exact signature algorithm differs between gateway vendors. signSubmit /
// signNotify below use the most common 易支付 conventions. Verify them against your
// provider's docs before going live — an incorrect sign silently fails verification.
type AggregatorProvider struct {
	Client *http.Client
}

func NewAggregatorProvider() *AggregatorProvider {
	return &AggregatorProvider{Client: &http.Client{Timeout: 15 * time.Second}}
}

func (a *AggregatorProvider) Name() string { return "aggregator" }

// CreateOrder POSTs a signed form to the gateway and parses the JSON response.
// A successful response looks like: {"code":1,"qrcode":"weixin://...","payurl":"...","trade_no":"..."}
func (a *AggregatorProvider) CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error) {
	form := url.Values{}
	form.Set("pid", in.PID)
	form.Set("type", in.Channel) // wxpay | alipay
	form.Set("out_trade_no", in.OutTradeNo)
	form.Set("notify_url", in.NotifyURL)
	form.Set("return_url", in.ReturnURL)
	form.Set("name", in.Name)
	form.Set("money", fmt.Sprintf("%.2f", in.Amount))
	form.Set("sign", a.signSubmit(in))
	form.Set("sign_type", "MD5")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.Gateway, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		QRCode  string `json:"qrcode"`
		PayURL  string `json:"payurl"`
		TradeNo string `json:"trade_no"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse gateway response: %w: %s", err, string(body))
	}
	if r.Code != 1 {
		return nil, fmt.Errorf("gateway rejected order: code=%d msg=%s", r.Code, r.Msg)
	}
	return &CreateOrderResult{QRContent: r.QRCode, PayURL: r.PayURL, TradeNo: r.TradeNo}, nil
}

// signSubmit is the 易支付 submit signature (adjust to your vendor if different).
func (a *AggregatorProvider) signSubmit(in CreateOrderInput) string {
	return md5hex(in.PID + in.OutTradeNo + fmt.Sprintf("%.2f", in.Amount) + in.Key)
}

// VerifyNotify validates the async callback. The gateway posts back form fields
// including out_trade_no, trade_no, money, pid and sign.
func (a *AggregatorProvider) VerifyNotify(rawBody []byte, headers http.Header, key string) (string, bool, error) {
	// Parse form-encoded body into params map
	vals, err := url.ParseQuery(string(rawBody))
	if err != nil {
		return "", false, fmt.Errorf("parse form body: %w", err)
	}
	params := map[string]string{}
	for k := range vals {
		params[k] = vals.Get(k)
	}

	outTradeNo := params["out_trade_no"]
	if outTradeNo == "" {
		return "", false, fmt.Errorf("missing out_trade_no")
	}
	// 易支付/codepay async notify signature. Verify against your gateway docs.
	sign := md5hex(
		md5hex(params["trade_no"]) +
			md5hex(params["money"]) +
			md5hex(params["pid"]) +
			key,
	)
	if !strings.EqualFold(sign, params["sign"]) {
		return outTradeNo, false, fmt.Errorf("signature mismatch")
	}
	return outTradeNo, true, nil
}
