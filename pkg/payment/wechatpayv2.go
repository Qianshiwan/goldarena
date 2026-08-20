package payment

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// WeChatPayV2Provider implements WeChat Pay v2 Native (扫码支付) API.
//
// This is the simpler alternative to v3 — it uses XML + MD5 signing and only
// requires mchid + appid + v2_key (no RSA private key, no APIv3 key, no cert).
//
// Supports both direct merchant mode and service-provider (sub-merchant) mode:
//   - Direct: mchid + appid belong to the merchant
//   - SP mode: appid is the service provider's, mchid is the SP's mch_id,
//     and sub_mchid is the sub-merchant's ID
type WeChatPayV2Provider struct {
	MchID    string
	AppID    string
	V2Key    string
	SubMchID string
	Client   *http.Client
}

func NewWeChatPayV2Provider(mchID, appID, v2Key, subMchID string) *WeChatPayV2Provider {
	return &WeChatPayV2Provider{
		MchID:    mchID,
		AppID:    appID,
		V2Key:    v2Key,
		SubMchID: subMchID,
		Client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (w *WeChatPayV2Provider) Name() string { return "wechatpay_v2" }

// CreateOrder calls POST /pay/unifiedorder (v2 XML API) to get a code_url for QR.
func (w *WeChatPayV2Provider) CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error) {
	amountCents := int64(in.Amount * 100)

	params := map[string]string{
		"appid":            w.AppID,
		"mch_id":           w.MchID,
		"nonce_str":        randomHex(32),
		"body":             in.Name,
		"out_trade_no":     in.OutTradeNo,
		"total_fee":        fmt.Sprintf("%d", amountCents),
		"spbill_create_ip": "127.0.0.1",
		"notify_url":       in.NotifyURL,
		"trade_type":       "NATIVE",
	}
	if w.SubMchID != "" {
		params["sub_mch_id"] = w.SubMchID
	}

	key := in.Key
	if key == "" {
		key = w.V2Key
	}
	params["sign"] = signV2(params, key)

	xmlBody := buildXML(params)

	const apiURL = "https://api.mch.weixin.qq.com/pay/unifiedorder"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(xmlBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("User-Agent", "GoldArena-Gateway/1.0")

	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	result, err := parseXMLMap(respBody)
	if err != nil {
		return nil, fmt.Errorf("parse XML response: %w: %s", err, string(respBody))
	}

	if result["return_code"] != "SUCCESS" {
		return nil, fmt.Errorf("wechatpay v2 error: return_code=%s, return_msg=%s",
			result["return_code"], result["return_msg"])
	}
	if result["result_code"] != "SUCCESS" {
		return nil, fmt.Errorf("wechatpay v2 error: result_code=%s, err_code=%s, err_code_des=%s",
			result["result_code"], result["err_code"], result["err_code_des"])
	}

	codeURL := result["code_url"]
	if codeURL == "" {
		return nil, fmt.Errorf("empty code_url in response: %s", string(respBody))
	}

	return &CreateOrderResult{
		QRContent: codeURL,
		TradeNo:   in.OutTradeNo,
	}, nil
}

// VerifyNotify parses the WeChat Pay v2 async callback (XML format).
func (w *WeChatPayV2Provider) VerifyNotify(rawBody []byte, headers http.Header, key string) (string, bool, error) {
	params, err := parseXMLMap(rawBody)
	if err != nil {
		return "", false, fmt.Errorf("parse callback XML: %w", err)
	}

	if params["return_code"] != "SUCCESS" {
		return "", false, fmt.Errorf("return_code=%s", params["return_code"])
	}

	sign := params["sign"]
	if sign == "" {
		return "", false, fmt.Errorf("missing sign in callback")
	}

	if key == "" {
		key = w.V2Key
	}

	expectedSign := signV2(params, key)
	if !strings.EqualFold(sign, expectedSign) {
		return "", false, fmt.Errorf("signature mismatch")
	}

	outTradeNo := params["out_trade_no"]
	if outTradeNo == "" {
		return "", false, fmt.Errorf("missing out_trade_no")
	}

	if params["result_code"] != "SUCCESS" {
		return outTradeNo, false, fmt.Errorf("result_code=%s", params["result_code"])
	}

	return outTradeNo, true, nil
}

// signV2 computes the WeChat Pay v2 MD5 signature.
func signV2(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf strings.Builder
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(params[k])
	}
	buf.WriteString("&key=")
	buf.WriteString(key)

	h := md5.Sum([]byte(buf.String()))
	return strings.ToUpper(hex.EncodeToString(h[:]))
}

// buildXML builds an XML string from a params map for WeChat Pay v2.
func buildXML(params map[string]string) string {
	var buf strings.Builder
	buf.WriteString("<xml>")
	for k, v := range params {
		buf.WriteString("<")
		buf.WriteString(k)
		buf.WriteString("><![CDATA[")
		buf.WriteString(v)
		buf.WriteString("]]></")
		buf.WriteString(k)
		buf.WriteString(">")
	}
	buf.WriteString("</xml>")
	return buf.String()
}

// parseXMLMap parses a flat <xml><key>value</key>...</xml> into a map.
// Uses encoding/xml which handles both plain text and CDATA sections.
func parseXMLMap(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var currentKey string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			currentKey = t.Name.Local
		case xml.CharData:
			if currentKey != "" && currentKey != "xml" {
				content := strings.TrimSpace(string(t))
				if content != "" {
					result[currentKey] = content
				}
			}
		case xml.EndElement:
			currentKey = ""
		}
	}
	return result, nil
}
