package imapbackend

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/artembaechka/mailbridge/internal/remote"
)

// attachmentPart is a downloaded blob for multipart/mixed composition.
type attachmentPart struct {
	fileName    string
	contentType string
	data        []byte
}

// BuildRFC822 builds an RFC822 message for IMAP clients from API metadata + HTML body.
// If attachments contains non-empty parts, the message is multipart/mixed (HTML first, then files).
func BuildRFC822(d *remote.MailDetail, htmlBody []byte, date time.Time, attachments []attachmentPart) []byte {
	var nonempty []attachmentPart
	for _, a := range attachments {
		if len(a.data) > 0 {
			nonempty = append(nonempty, a)
		}
	}
	if len(nonempty) == 0 {
		return buildRFC822HTMLOnly(d, htmlBody, date)
	}
	return buildRFC822Multipart(d, htmlBody, date, nonempty)
}

func buildRFC822HTMLOnly(d *remote.MailDetail, htmlBody []byte, date time.Time) []byte {
	from := formatAddr(d.FromName, d.FromEmail)
	to := d.To
	if to == "" {
		to = "undisclosed@local"
	}
	subj := d.Subject
	if subj == "" {
		subj = "(no subject)"
	}
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(sanitizeHeader(from))
	b.WriteString("\r\nTo: ")
	b.WriteString(sanitizeHeader(to))
	b.WriteString("\r\nSubject: ")
	b.WriteString(encodeSubject(subj))
	b.WriteString("\r\nDate: ")
	b.WriteString(date.Format(time.RFC1123Z))
	b.WriteString("\r\nMIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	if d.ID > 0 {
		fmt.Fprintf(&b, "Message-ID: <%s>\r\n", remoteBridgeMessageID(d.ID))
		fmt.Fprintf(&b, "X-Remote-Mail-Id: %d\r\n", d.ID)
	}
	b.WriteString("\r\n")
	b.Write(htmlPayload(d, htmlBody))
	return []byte(b.String())
}

func buildRFC822Multipart(d *remote.MailDetail, htmlBody []byte, date time.Time, attachments []attachmentPart) []byte {
	var mpBody bytes.Buffer
	w := multipart.NewWriter(&mpBody)

	h := textproto.MIMEHeader{}
	h.Set("Content-Type", "text/html; charset=UTF-8")
	h.Set("Content-Transfer-Encoding", "8bit")
	pw, err := w.CreatePart(h)
	if err == nil {
		_, _ = pw.Write(htmlPayload(d, htmlBody))
	}

	for _, a := range attachments {
		ah := textproto.MIMEHeader{}
		ct := a.contentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		ah.Set("Content-Type", ct)
		ah.Set("Content-Disposition", mimeContentDispositionAttachment(a.fileName))
		ah.Set("Content-Transfer-Encoding", "base64")
		aw, err := w.CreatePart(ah)
		if err != nil {
			continue
		}
		writeMIMEBase64Lines(aw, a.data)
	}
	_ = w.Close()

	boundary := w.Boundary()
	ct := mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": boundary})

	from := formatAddr(d.FromName, d.FromEmail)
	to := d.To
	if to == "" {
		to = "undisclosed@local"
	}
	subj := d.Subject
	if subj == "" {
		subj = "(no subject)"
	}
	var out strings.Builder
	out.WriteString("From: ")
	out.WriteString(sanitizeHeader(from))
	out.WriteString("\r\nTo: ")
	out.WriteString(sanitizeHeader(to))
	out.WriteString("\r\nSubject: ")
	out.WriteString(encodeSubject(subj))
	out.WriteString("\r\nDate: ")
	out.WriteString(date.Format(time.RFC1123Z))
	out.WriteString("\r\nMIME-Version: 1.0\r\n")
	fmt.Fprintf(&out, "Content-Type: %s\r\n", ct)
	if d.ID > 0 {
		fmt.Fprintf(&out, "Message-ID: <%s>\r\n", remoteBridgeMessageID(d.ID))
		fmt.Fprintf(&out, "X-Remote-Mail-Id: %d\r\n", d.ID)
	}
	out.WriteString("\r\n")
	out.Write(mpBody.Bytes())
	return []byte(out.String())
}

// remoteBridgeMessageID is referenced in In-Reply-To when clients reply via SMTP.
func remoteBridgeMessageID(mailID int) string {
	return fmt.Sprintf("xyecoc-mail-%d@mailbridge", mailID)
}

func htmlPayload(d *remote.MailDetail, htmlBody []byte) []byte {
	if len(htmlBody) > 0 {
		return htmlBody
	}
	if d.Message != "" {
		return []byte(d.Message)
	}
	return []byte("<html><body></body></html>")
}

func mimeContentDispositionAttachment(fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		fileName = "attachment"
	}
	esc := strings.ReplaceAll(fileName, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	return fmt.Sprintf(`attachment; filename="%s"`, esc)
}

func writeMIMEBase64Lines(w io.Writer, data []byte) {
	const lineLen = 76
	enc := base64.StdEncoding
	buf := make([]byte, enc.EncodedLen(len(data)))
	enc.Encode(buf, data)
	for i := 0; i < len(buf); i += lineLen {
		j := i + lineLen
		if j > len(buf) {
			j = len(buf)
		}
		_, _ = w.Write(buf[i:j])
		_, _ = w.Write([]byte("\r\n"))
	}
}

func formatAddr(name, email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "unknown@invalid"
	}
	if name == "" {
		return email
	}
	addr := mail.Address{Name: name, Address: email}
	return addr.String()
}

func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func encodeSubject(s string) string {
	// minimal MIME encoded-word for non-ascii
	if isASCII(s) {
		return s
	}
	return fmt.Sprintf("=?UTF-8?B?%s?=", base64Std(s))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func mimeTypeForAttachment(a remote.Attachment) string {
	ext := strings.TrimSpace(a.Extension)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	ct := mime.TypeByExtension(strings.ToLower(ext))
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func attachmentDisplayName(a remote.Attachment) string {
	if strings.TrimSpace(a.FileName) != "" {
		return strings.TrimSpace(a.FileName)
	}
	ext := strings.TrimSpace(a.Extension)
	if ext != "" {
		ext = strings.TrimPrefix(ext, ".")
		return "attachment." + ext
	}
	return fmt.Sprintf("attachment-%d", a.ID)
}
