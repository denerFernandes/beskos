// Package notify sends outbound notifications (e-mail) for the control plane. Optional: when not
// configured it is a no-op, so in-app notifications still work on their own.
package notify

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Email is a minimal SMTP submission sender. Nil when unconfigured.
//
// Two transports, because Stalwart differs by port (measured 2026-07-15 against 0.16.4):
//   - :465 implicit TLS — works, and is what we use.
//   - :587 STARTTLS — the listener drops the connection here; do not assume it works.
//
// implicit=true dials TLS straight away (no STARTTLS handshake).
type Email struct {
	host, port, from, user, pass string
	insecure                     bool
	implicit                     bool
}

// NewEmail builds the sender, or returns nil (disabled) when host/from are missing.
// Port 465 turns on implicit TLS automatically — it is the only meaning that port has.
func NewEmail(host, port, from, user, pass string, insecure bool) *Email {
	if host == "" || from == "" {
		return nil
	}
	if port == "" {
		port = "465"
	}
	return &Email{
		host: host, port: port, from: from, user: user, pass: pass,
		insecure: insecure, implicit: port == "465",
	}
}

// dial opens the SMTP client, with implicit TLS when the port asks for it.
func (e *Email) dial() (*smtp.Client, error) {
	addr := e.host + ":" + e.port
	// insecure só em dev (cert self-signed). Produção: certificado válido (insecure=false).
	tlsCfg := &tls.Config{ServerName: e.host, InsecureSkipVerify: e.insecure}
	if e.implicit {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsCfg)
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(conn, e.host)
	}
	c, err := smtp.Dial(addr)
	if err != nil {
		return nil, err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsCfg); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// SendHTML delivers a message with both an HTML and a plain-text part (multipart/alternative).
// Clients that refuse HTML — and screen readers — still get a readable message.
func (e *Email) SendHTML(to, subject, html, text string) error {
	boundary := "beskos-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := "--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" +
		strings.ReplaceAll(text, "\n", "\r\n") + "\r\n" +
		"--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" +
		strings.ReplaceAll(html, "\n", "\r\n") + "\r\n" +
		"--" + boundary + "--\r\n"
	return e.send(to, subject, `multipart/alternative; boundary="`+boundary+`"`, body)
}

// Send delivers a plain-text message. Best-effort: the caller logs errors and continues.
func (e *Email) Send(to, subject, body string) error {
	return e.send(to, subject, "text/plain; charset=UTF-8", strings.ReplaceAll(body, "\n", "\r\n"))
}

// send monta o envelope e entrega. O corpo já vem com o Content-Type certo e as quebras em CRLF.
func (e *Email) send(to, subject, contentType, body string) error {
	// Validate the recipient address before it enters the To: header — defense in depth against
	// header injection (a CRLF in `to` could forge headers). ParseAddress rejects control chars.
	addr, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	to = addr.Address
	c, err := e.dial()
	if err != nil {
		return err
	}
	defer c.Close()
	if e.user != "" {
		if err := c.Auth(smtp.PlainAuth("", e.user, e.pass, e.host)); err != nil {
			return err
		}
	}
	if err := c.Mail(e.from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	// Subject is encoded: accents in "Nova tarefa atribuída" would be mangled raw.
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: %s\r\n\r\n%s\r\n",
		e.from, to, mime.QEncoding.Encode("UTF-8", subject), time.Now().Format(time.RFC1123Z),
		contentType, body)
	if _, err := wc.Write([]byte(msg)); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}
