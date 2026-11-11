package services

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"mime"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// Mailer sends email through any SMTP server (Gmail: smtp.gmail.com:587 + App Password).
type Mailer struct {
	Host, Port, User, Pass string
}

type Attachment struct {
	Filename string
	Path     string
}

func (m *Mailer) Configured() bool {
	return m != nil && m.User != "" && m.Pass != ""
}

// Send delivers one email. Returns an error (never panics) so callers can report per recipient.
func (m *Mailer) Send(to, subject, htmlBody, textBody string, attachments []Attachment) error {
	if !m.Configured() {
		return errors.New("email is not configured: set SMTP_USER and SMTP_PASS in backend/.env")
	}
	msg, err := buildMessage(m.User, to, subject, htmlBody, textBody, attachments)
	if err != nil {
		return err
	}
	return m.deliver(to, msg)
}

func (m *Mailer) deliver(to string, msg []byte) error {
	addr := net.JoinHostPort(m.Host, m.Port)
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach mail server: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))
	if m.Port == "465" { // implicit TLS
		conn = tls.Client(conn, &tls.Config{ServerName: m.Host})
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail server error: %w", err)
	}
	defer c.Close()
	if m.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
				return fmt.Errorf("TLS error: %w", err)
			}
		}
	}
	if err := c.Auth(smtp.PlainAuth("", m.User, m.Pass, m.Host)); err != nil {
		return fmt.Errorf("mail login failed (check SMTP_USER / SMTP_PASS app password): %w", err)
	}
	if err := c.Mail(m.User); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("recipient rejected: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

var headerCleaner = strings.NewReplacer("\r", " ", "\n", " ")

func buildMessage(from, to, subject, htmlBody, textBody string, atts []Attachment) ([]byte, error) {
	var b bytes.Buffer
	mixed := "signora-mixed-" + randomHex()
	alt := "signora-alt-" + randomHex()

	header := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	header("From", "Signora <"+from+">")
	header("To", headerCleaner.Replace(to))
	header("Subject", mime.QEncoding.Encode("utf-8", headerCleaner.Replace(subject)))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/mixed; boundary="`+mixed+`"`)
	b.WriteString("\r\n")

	b.WriteString("--" + mixed + "\r\n")
	b.WriteString(`Content-Type: multipart/alternative; boundary="` + alt + `"` + "\r\n\r\n")
	writeTextPart(&b, alt, "text/plain", textBody)
	writeTextPart(&b, alt, "text/html", htmlBody)
	b.WriteString("--" + alt + "--\r\n")

	for _, a := range atts {
		data, err := os.ReadFile(a.Path)
		if err != nil {
			return nil, fmt.Errorf("cannot read attachment %s: %w", a.Filename, err)
		}
		name := headerCleaner.Replace(a.Filename)
		ctype := mime.FormatMediaType("application/pdf", map[string]string{"name": name})
		disp := mime.FormatMediaType("attachment", map[string]string{"filename": name})
		if ctype == "" {
			ctype = "application/pdf"
		}
		if disp == "" {
			disp = "attachment"
		}
		b.WriteString("--" + mixed + "\r\n")
		b.WriteString("Content-Type: " + ctype + "\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("Content-Disposition: " + disp + "\r\n\r\n")
		writeBase64(&b, data)
	}
	b.WriteString("--" + mixed + "--\r\n")
	return b.Bytes(), nil
}

func writeTextPart(b *bytes.Buffer, boundary, ctype, body string) {
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: " + ctype + "; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	writeBase64(b, []byte(body))
}

func writeBase64(b *bytes.Buffer, data []byte) {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
}

func randomHex() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// ---------- Email templates (responsive: fluid 600px card, stacked button on phones) ----------

var esc = html.EscapeString

func emailLayout(inner string) string {
	return `<!DOCTYPE html><html><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<style>@media only screen and (max-width:620px){.wrap{width:100% !important}.pad{padding:22px 18px !important}.btn{display:block !important}}</style>` +
		`</head><body style="margin:0;padding:16px 8px;background:#f1f5f9;font-family:Arial,Helvetica,sans-serif;">` +
		`<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr><td align="center">` +
		`<table role="presentation" class="wrap" width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%;background:#ffffff;border-radius:12px;overflow:hidden;">` +
		`<tr><td style="background:#4f46e5;padding:26px;text-align:center;"><div style="color:#ffffff;font-size:24px;font-weight:bold;">Signora</div>` +
		`<div style="color:#e0e7ff;font-size:13px;margin-top:4px;">Electronic Document Signing</div></td></tr>` +
		`<tr><td class="pad" style="padding:32px 30px;color:#334155;font-size:15px;line-height:1.6;">` + inner + `</td></tr>` +
		`<tr><td style="background:#f8fafc;padding:16px;text-align:center;color:#94a3b8;font-size:12px;">If you did not expect this email you can safely ignore it.</td></tr>` +
		`</table></td></tr></table></body></html>`
}

func docBox(title string) string {
	return `<div style="background:#eef2ff;border-left:4px solid #4f46e5;padding:14px 16px;border-radius:6px;margin:18px 0;font-weight:bold;color:#1e293b;word-break:break-word;">` + esc(title) + `</div>`
}

func SigningRequestEmail(signerName, senderName, docTitle, link string, expires time.Time, reminder bool) (subject, htmlBody, textBody string) {
	subject = "Signature requested: " + docTitle
	intro := "has requested your signature on:"
	if reminder {
		subject = "Reminder: " + subject
		intro = "is still waiting for your signature on:"
	}
	exp := expires.Format("2 January 2006")

	inner := `<p style="margin:0 0 14px;">Hi <strong>` + esc(signerName) + `</strong>,</p>` +
		`<p style="margin:0 0 6px;"><strong>` + esc(senderName) + `</strong> ` + intro + `</p>` + docBox(docTitle) +
		`<p style="text-align:center;margin:26px 0;"><a class="btn" href="` + esc(link) + `" style="display:inline-block;background:#4f46e5;color:#ffffff;text-decoration:none;padding:14px 32px;border-radius:8px;font-weight:bold;">Review &amp; Sign</a></p>` +
		`<p style="font-size:13px;color:#64748b;word-break:break-all;margin:0 0 10px;">If the button does not work, copy this link into your browser:<br>` + esc(link) + `</p>` +
		`<p style="font-size:13px;color:#dc2626;margin:0;">This link expires on ` + exp + ` and can only be used once.</p>`

	textBody = fmt.Sprintf("Hi %s,\n\n%s %s\n\"%s\"\n\nOpen this link to review and sign:\n%s\n\nThis link expires on %s and can only be used once.\n",
		signerName, senderName, intro, docTitle, link, exp)
	return subject, emailLayout(inner), textBody
}

func CompletionEmail(recipientName, docTitle string, attached bool) (subject, htmlBody, textBody string) {
	subject = "Completed: " + docTitle
	note := "The signed document is ready. Please ask the sender for a copy of the signed PDF."
	if attached {
		note = "The fully signed PDF, including a Certificate of Completion and audit trail, is attached to this email."
	}
	inner := `<p style="margin:0 0 14px;">Hi <strong>` + esc(recipientName) + `</strong>,</p>` +
		`<p style="margin:0 0 6px;">Good news! Everyone has signed:</p>` + docBox(docTitle) +
		`<p style="margin:0;">` + esc(note) + `</p>`
	textBody = fmt.Sprintf("Hi %s,\n\nEveryone has signed \"%s\".\n\n%s\n", recipientName, docTitle, note)
	return subject, emailLayout(inner), textBody
}
