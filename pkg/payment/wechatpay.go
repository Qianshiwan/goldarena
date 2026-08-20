package payment

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WeChatPayProvider implements WeChat Pay v3 Native (扫码支付) API.
//
// Flow: platform creates order -> calls /v3/pay/transactions/native -> gets
// code_url -> renders QR -> user scans with WeChat -> WeChat sends async
// callback to notify_url (JSON, AES-256-GCM encrypted) -> provider decrypts
// & verifies -> wallet credited.
//
// Required credentials (from pay.weixin.qq.cn merchant platform):
//   - mchid:        商户号 (10-digit)
//   - appid:        关联的公众号/小程序 appid
//   - serial_no:    商户API证书序列号 (40-hex)
//   - private_key:  商户API私钥 PEM (apiclient_key.pem content)
//   - api_v3_key:   APIv3密钥 (32-byte string, set in merchant platform under API安全)
type WeChatPayProvider struct {
	MchID      string
	AppID      string
	SerialNo   string
	PrivateKey *rsa.PrivateKey
	APIV3Key   string
	Client     *http.Client
}

// NewWeChatPayProvider parses the PEM private key and returns a ready provider.
func NewWeChatPayProvider(mchID, appID, serialNo, privateKeyPEM, apiV3Key string) (*WeChatPayProvider, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM private key — make sure apiclient_key.pem is PKCS#8 or PKCS#1 RSA")
	}

	var rsaKey *rsa.PrivateKey
	// Try PKCS#8 first, then PKCS#1
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		var ok bool
		rsaKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is not RSA")
		}
	} else if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		rsaKey = key
	} else {
		return nil, fmt.Errorf("failed to parse private key (tried PKCS#8 and PKCS#1): %w", err)
	}

	return &WeChatPayProvider{
		MchID:      mchID,
		AppID:      appID,
		SerialNo:   serialNo,
		PrivateKey: rsaKey,
		APIV3Key:   apiV3Key,
		Client:     &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (w *WeChatPayProvider) Name() string { return "wechatpay" }

// CreateOrder calls POST /v3/pay/transactions/native to get a code_url for QR.
func (w *WeChatPayProvider) CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error) {
	// WeChat Pay amount is in cents (分)
	amountCents := int64(in.Amount * 100)

	body := map[string]interface{}{
		"appid":        w.AppID,
		"mchid":        w.MchID,
		"description":  in.Name,
		"out_trade_no": in.OutTradeNo,
		"notify_url":   in.NotifyURL,
		"amount": map[string]interface{}{
			"total":    amountCents,
			"currency": "CNY",
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}

	const apiPath = "/v3/pay/transactions/native"
	const apiURL = "https://api.mch.weixin.qq.com" + apiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// Sign the request
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonceStr := randomHex(32)
	signature := w.signRequest("POST", apiPath, timestamp, nonceStr, string(bodyBytes))

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GoldArena-Gateway/1.0")
	req.Header.Set("Authorization", fmt.Sprintf(
		"WECHATPAY2-SHA256-RSA2048 mchid=\"%s\",nonce_str=\"%s\",timestamp=\"%s\",serial_no=\"%s\",signature=\"%s\"",
		w.MchID, nonceStr, timestamp, w.SerialNo, signature,
	))

	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wechatpay API error: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w: %s", err, string(respBody))
	}
	if result.CodeURL == "" {
		return nil, fmt.Errorf("empty code_url in response: %s", string(respBody))
	}

	return &CreateOrderResult{
		QRContent: result.CodeURL, // e.g. weixin://wxpay/bizpayurl?pr=xxx
		TradeNo:   in.OutTradeNo,
	}, nil
}

// VerifyNotify decrypts the WeChat Pay v3 async callback.
//
// The callback is a POST with JSON body:
//   { "id":"...", "create_time":"...", "event_type":"TRANSACTION.SUCCESS",
//     "resource_type":"encrypt-resource",
//     "resource": { "algorithm":"AEAD_AES_256_GCM",
//                   "ciphertext":"...", "nonce":"...", "associated_data":"..." } }
//
// We decrypt resource.ciphertext with AES-256-GCM using api_v3_key, then
// parse the plaintext JSON to extract out_trade_no and trade_state.
func (w *WeChatPayProvider) VerifyNotify(rawBody []byte, headers http.Header, key string) (string, bool, error) {
	// Parse the encrypted callback envelope
	var envelope struct {
		ID          string `json:"id"`
		EventType   string `json:"event_type"`
		Resource    struct {
			Algorithm       string `json:"algorithm"`
			Ciphertext      string `json:"ciphertext"`
			Nonce           string `json:"nonce"`
			AssociatedData  string `json:"associated_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return "", false, fmt.Errorf("parse callback JSON: %w", err)
	}
	if envelope.Resource.Ciphertext == "" {
		return "", false, fmt.Errorf("empty ciphertext in callback resource")
	}

	// Decrypt the resource using AES-256-GCM with APIv3 key
	plaintext, err := w.decryptResource(
		envelope.Resource.Ciphertext,
		envelope.Resource.Nonce,
		envelope.Resource.AssociatedData,
	)
	if err != nil {
		return "", false, fmt.Errorf("decrypt callback resource: %w", err)
	}

	// Parse the decrypted payment result
	var result struct {
		OutTradeNo string `json:"out_trade_no"`
		TradeState string `json:"trade_state"`
		TradeType  string `json:"trade_type"`
		Amount     struct {
			Total    int64  `json:"total"`
			Currency string `json:"currency"`
		} `json:"amount"`
	}
	if err := json.Unmarshal([]byte(plaintext), &result); err != nil {
		return "", false, fmt.Errorf("parse decrypted result: %w", err)
	}

	if result.TradeState != "SUCCESS" {
		return result.OutTradeNo, false, fmt.Errorf("trade_state=%s (not SUCCESS)", result.TradeState)
	}

	return result.OutTradeNo, true, nil
}

// signRequest builds the WECHATPAY2-SHA256-RSA2048 signature for API calls.
// The signing string is: METHOD\nPATH\nTIMESTAMP\nNONCE\nBODY\n
func (w *WeChatPayProvider) signRequest(method, path, timestamp, nonceStr, body string) string {
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n", method, path, timestamp, nonceStr, body)
	h := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPKCS1v15(rand.Reader, w.PrivateKey, crypto.SHA256, h[:]) //nolint:staticcheck
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// decryptResource decrypts AES-256-GCM ciphertext using the APIv3 key.
func (w *WeChatPayProvider) decryptResource(ciphertextB64, nonce, associatedData string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", fmt.Errorf("base64 decode ciphertext: %w", err)
	}

	if len(w.APIV3Key) != 32 {
		return "", fmt.Errorf("api_v3_key must be 32 bytes, got %d", len(w.APIV3Key))
	}

	block, err := aes.NewCipher([]byte(w.APIV3Key))
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, []byte(nonce), ciphertext, []byte(associatedData))
	if err != nil {
		return "", fmt.Errorf("GCM decrypt: %w (wrong APIv3 key?)", err)
	}

	return string(plaintext), nil
}

// randomHex generates a random hex string of n characters (n/2 bytes of entropy).
func randomHex(n int) string {
	b := make([]byte, n/2)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
