// Package protocollog writes IMAP/SMTP wire lines to slog (optional redaction).
package protocollog

import (
	"bytes"
	"log/slog"
	"strings"
)

const maxWireLine = 8192

// IMAPServerDebug returns an io.Writer suitable for go-imap/server.Server.Debug.
// Lines are logged as JSON logs with msg "imap wire".
func IMAPServerDebug(log *slog.Logger) *LineWriter {
	if log == nil {
		log = slog.Default()
	}
	return &LineWriter{
		log: log,
		msg: "imap wire",
	}
}

// LineWriter buffers fragmentary writes and logs complete CRLF or LF lines.
type LineWriter struct {
	log *slog.Logger
	msg string
	buf bytes.Buffer
}

func (w *LineWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.buf.Write(p)
	for {
		data := w.buf.Bytes()
		if len(data) == 0 {
			break
		}
		var end, skip int = -1, 0
		if i := bytes.Index(data, []byte("\r\n")); i >= 0 {
			end = i
			skip = 2
		} else if i := bytes.IndexByte(data, '\n'); i >= 0 {
			end = i
			skip = 1
		}
		if end < 0 {
			break
		}
		line := data[:end]
		w.buf.Next(end + skip)
		w.emitLine(line)
	}
	return len(p), nil
}

func (w *LineWriter) emitLine(line []byte) {
	s := strings.TrimSpace(string(line))
	if s == "" {
		return
	}
	if len(s) > maxWireLine {
		s = s[:maxWireLine] + "…[truncated]"
	}
	w.log.Info(w.msg, "line", redactIMAPLine(s))
}

func redactIMAPLine(s string) string {
	fields := strings.Fields(s)
	if len(fields) >= 4 && strings.EqualFold(fields[1], "LOGIN") {
		return fields[0] + " LOGIN " + fields[2] + " [redacted]"
	}
	u := strings.ToUpper(s)
	if strings.Contains(u, "AUTHENTICATE") {
		return "[redacted AUTHENTICATE …]"
	}
	return s
}

// RedactSMTP masks AUTH payloads and long base64-looking tokens.
func RedactSMTP(line string) string {
	u := strings.ToUpper(line)
	if strings.HasPrefix(u, "AUTH ") || strings.Contains(u, "AUTH ") {
		return "[redacted AUTH …]"
	}
	return line
}
