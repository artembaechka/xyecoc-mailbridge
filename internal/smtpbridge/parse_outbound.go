package smtpbridge

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // decode common charsets in MIME parts
)

var reMailBridgeInReply = regexp.MustCompile(`(?i)<xyecoc-mail-(\d+)@mailbridge>`)

// parseOutboundMail extracts Subject, HTML body, reply mail id, and API-style attaches
// (filename + base64 content) for mail/reply-confirm and mail/message-new.
func parseOutboundMail(raw []byte) (subject, html, replyMailID string, attaches []any, err error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		subj, body, err2 := parseMailFromSMTPData(raw)
		return subj, body, "", nil, err2
	}

	subject = entity.Header.Get("Subject")
	replyMailID = extractReplyMailID(entity.Header)

	var htmlPart, plainPart string
	var media []map[string]any

	_ = entity.Walk(func(_ []int, p *message.Entity, werr error) error {
		if werr != nil {
			return nil
		}
		ct := p.Header.Get("Content-Type")
		mediaType, ctParams, _ := mime.ParseMediaType(ct)
		disp, dispParams, _ := p.Header.ContentDisposition()
		fname := pickFilename(dispParams, ctParams)

		switch {
		case strings.HasPrefix(mediaType, "text/html"):
			b, err := io.ReadAll(p.Body)
			if err != nil {
				return nil
			}
			if len(b) == 0 {
				return nil
			}
			// Do not treat main HTML as attachment; only if explicitly attachment.
			if strings.EqualFold(disp, "attachment") {
				media = append(media, attachPayload(fname, mediaType, b))
				return nil
			}
			htmlPart = string(b)
		case strings.HasPrefix(mediaType, "text/plain"):
			b, err := io.ReadAll(p.Body)
			if err != nil {
				return nil
			}
			if len(b) == 0 {
				return nil
			}
			if strings.EqualFold(disp, "attachment") {
				media = append(media, attachPayload(fname, mediaType, b))
				return nil
			}
			if plainPart == "" {
				plainPart = string(b)
			}
		default:
			if shouldEncodeAsAttachment(disp, fname, mediaType) {
				b, err := io.ReadAll(p.Body)
				if err != nil || len(b) == 0 {
					return nil
				}
				if fname == "" {
					fname = guessFilename(mediaType)
				}
				media = append(media, attachPayload(fname, mediaType, b))
			}
		}
		return nil
	})

	for _, m := range media {
		attaches = append(attaches, m)
	}

	switch {
	case strings.TrimSpace(htmlPart) != "":
		html = strings.TrimSpace(htmlPart)
	case strings.TrimSpace(plainPart) != "":
		html = "<pre>" + htmlEscPlain(plainPart) + "</pre>"
	default:
		_, fb, err2 := parseMailFromSMTPData(raw)
		if err2 == nil && fb != "" {
			html = fb
		} else {
			html = "<html><body></body></html>"
		}
	}

	if strings.TrimSpace(subject) == "" {
		subject = "(no subject)"
	}
	return subject, html, replyMailID, attaches, nil
}

func attachPayload(filename, mediaType string, raw []byte) map[string]any {
	fn := basenameOnly(filename)
	if fn == "" {
		fn = guessFilename(mediaType)
	}
	return map[string]any{
		"filename": fn,
		"content":  base64.StdEncoding.EncodeToString(raw),
	}
}

func pickFilename(dispParams, ctParams map[string]string) string {
	if dispParams != nil {
		if v := dispParams["filename"]; v != "" {
			return decodeRFC2047Filename(v)
		}
	}
	if ctParams != nil {
		if v := ctParams["name"]; v != "" {
			return decodeRFC2047Filename(v)
		}
	}
	return ""
}

func decodeRFC2047Filename(s string) string {
	s = strings.TrimSpace(s)
	// Minimal: strip quotes; full encoded-word decode can be added if needed
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

func shouldEncodeAsAttachment(disp, fname, mediaType string) bool {
	d := strings.ToLower(strings.TrimSpace(disp))
	mt := strings.ToLower(mediaType)
	if d == "attachment" {
		return true
	}
	if d == "inline" && fname != "" && !strings.HasPrefix(mt, "text/") {
		return true
	}
	if fname != "" && (strings.HasPrefix(mt, "image/") || strings.HasPrefix(mt, "audio/") || strings.HasPrefix(mt, "video/")) {
		return true
	}
	return false
}

func guessFilename(mediaType string) string {
	mt := strings.ToLower(mediaType)
	switch {
	case strings.HasPrefix(mt, "image/jpeg"):
		return "image.jpg"
	case strings.HasPrefix(mt, "image/png"):
		return "image.png"
	case strings.HasPrefix(mt, "image/gif"):
		return "image.gif"
	case strings.HasPrefix(mt, "image/webp"):
		return "image.webp"
	default:
		exts, _ := mime.ExtensionsByType(mediaType)
		if len(exts) > 0 {
			return "attachment" + exts[0]
		}
		return "attachment.bin"
	}
}

// Basename sanitizes path components from clients that send full paths.
func basenameOnly(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func extractReplyMailID(h message.Header) string {
	if v := strings.TrimSpace(h.Get("X-Remote-Mail-Id")); v != "" {
		return v
	}
	for _, key := range []string{"In-Reply-To", "References"} {
		v := h.Get(key)
		if m := reMailBridgeInReply.FindStringSubmatch(v); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

func htmlEscPlain(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
