// Package mail sends registration verification codes. When an SMTP server is
// configured it sends a real email (implicit TLS on port 465, or STARTTLS on
// 587/25); otherwise (dev mode) it logs the code and, if DevPrintCode is
// enabled, returns it so the frontend can display it for local testing.
// Set DevPrintCode=false in production.
package mail

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Config holds SMTP credentials and dev-mode behaviour.
type Config struct {
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	From     string
	UseSSL   bool // implicit TLS (port 465); otherwise STARTTLS (587/25)
	// DevPrintCode, when true and SMTP is not configured, makes SendCode return
	// the plaintext code so the caller can show it in a dev UI. Never enable in prod.
	DevPrintCode bool
}

// Sender sends verification emails.
type Sender struct {
	cfg Config
}

// NewSender builds a Sender from Config.
func NewSender(cfg Config) *Sender {
	if cfg.From == "" {
		cfg.From = "GoldArena <noreply@goldarena.com>"
	}
	return &Sender{cfg: cfg}
}

func (s *Sender) configured() bool {
	return s.cfg.SMTPHost != "" && s.cfg.SMTPPort > 0 && s.cfg.SMTPUser != "" && s.cfg.SMTPPass != ""
}

// SendCode delivers a registration verification code to `to`. In dev mode it
// logs the code and may return it as devCode for UI display. err is non-nil
// only when a configured SMTP server actually fails to send.
func (s *Sender) SendCode(to, code string) (string, error) {
	subject := "金归子现货模拟交易 - 邮箱验证码"
	body := fmt.Sprintf(
		"您好，\n\n您正在注册金归子现货模拟交易游戏平台，邮箱验证码为：%s\n该验证码 5 分钟内有效，请勿转发给他人。\n\n若非本人操作，请忽略本邮件。",
		code,
	)
	html := fmt.Sprintf(`<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;max-width:480px;margin:0 auto;padding:24px;background:#0f1115;color:#e8e8e8;border-radius:12px">
  <h2 style="color:#e6b800;margin:0 0 12px">金归子现货模拟交易游戏平台</h2>
  <p>您好，您正在注册金归子现货模拟交易游戏平台。</p>
  <p>您的邮箱验证码为：</p>
  <div style="font-size:32px;letter-spacing:8px;font-weight:700;color:#e6b800;margin:12px 0">%s</div>
  <p style="color:#9aa0a6;font-size:13px">该验证码 5 分钟内有效，请勿转发给他人。若非本人操作，请忽略本邮件。</p>
</div>`, code)
	return s.deliver(to, code, subject, body, html)
}

// SendResetCode delivers a password-reset verification code to `to`.
func (s *Sender) SendResetCode(to, code string) (string, error) {
	subject := "金归子现货模拟交易 - 重置密码验证码"
	body := fmt.Sprintf(
		"您好，\n\n您正在重置金归子现货模拟交易游戏平台的登录密码，验证码为：%s\n该验证码 5 分钟内有效，请勿转发给他人。\n\n若非本人操作，请尽快修改密码或联系客服。",
		code,
	)
	html := fmt.Sprintf(`<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;max-width:480px;margin:0 auto;padding:24px;background:#0f1115;color:#e8e8e8;border-radius:12px">
  <h2 style="color:#e6b800;margin:0 0 12px">金归子现货模拟交易游戏平台</h2>
  <p>您好，您正在重置金归子现货模拟交易游戏平台的登录密码。</p>
  <p>您的重置验证码为：</p>
  <div style="font-size:32px;letter-spacing:8px;font-weight:700;color:#e6b800;margin:12px 0">%s</div>
  <p style="color:#9aa0a6;font-size:13px">该验证码 5 分钟内有效，请勿转发给他人。若非本人操作，请尽快修改密码或联系客服。</p>
</div>`, code)
	return s.deliver(to, code, subject, body, html)
}

// deliver builds the MIME message and sends it. In dev mode (SMTP not
// configured) it logs the code and, when DevPrintCode is enabled, returns the
// plaintext code for UI display. err is non-nil only when a configured SMTP
// server actually fails to send.
func (s *Sender) deliver(to, code, subject, body, html string) (devCode string, err error) {
	if !s.configured() {
		log.Printf("[mail:dev] verification code for %s = %s", to, code)
		if s.cfg.DevPrintCode {
			return code, nil
		}
		return "", nil
	}

	msg := buildMIME(to, s.cfg.From, subject, body, html)
	if s.cfg.UseSSL {
		if e := s.sendSSL(to, msg); e != nil {
			return "", e
		}
		return "", nil
	}

	// STARTTLS (ports 587 / 25)
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	if e := smtp.SendMail(addr, auth, fromAddr(s.cfg.From), []string{to}, []byte(msg)); e != nil {
		return "", e
	}
	return "", nil
}

// sendSSL opens an implicit-TLS connection (port 465) and sends the message.
func (s *Sender) sendSSL(to, msg string) error {
	host := s.cfg.SMTPHost
	addr := fmt.Sprintf("%s:%d", host, s.cfg.SMTPPort)
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Quit()

	auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, host)
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(fromAddr(s.cfg.From)); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	return w.Close()
}

// fromAddr extracts the bare email address from a "Name <addr>" From header.
func fromAddr(from string) string {
	if a, err := mail.ParseAddress(from); err == nil {
		return a.Address
	}
	return from
}

// buildMIME builds a multipart/alternative (text + HTML) message.
func buildMIME(to, from, subject, body, html string) string {
	boundary := "goldarena-boundary-" + fmt.Sprintf("%d", time.Now().UnixNano())
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(subject)) + "?=\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=" + boundary + "\r\n")
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(body)))
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(base64.StdEncoding.EncodeToString([]byte(html)))
	b.WriteString("\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}
