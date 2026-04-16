package smtpbridge

import (
	"strings"
	"testing"
)

func TestExtractReplyMailID_XHeader(t *testing.T) {
	raw := "Subject: Re: hi\r\nX-Remote-Mail-Id: 317515\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n<p>x</p>"
	sub, html, id, att, err := parseOutboundMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(att) != 0 {
		t.Fatalf("attaches=%v", att)
	}
	if id != "317515" {
		t.Fatalf("id=%q", id)
	}
	if !strings.Contains(html, "<p>x</p>") {
		t.Fatalf("html=%q", html)
	}
	if sub != "Re: hi" {
		t.Fatalf("subj=%q", sub)
	}
}

func TestExtractReplyMailID_MessageID(t *testing.T) {
	raw := "Subject: Re: hi\r\nIn-Reply-To: <xyecoc-mail-317515@mailbridge>\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nbody"
	_, _, id, _, err := parseOutboundMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if id != "317515" {
		t.Fatalf("id=%q", id)
	}
}

func TestParseOutbound_attachmentPart(t *testing.T) {
	raw := "Subject: t\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=BOUNDARY\r\n" +
		"\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n" +
		"\r\n" +
		"<p>x</p>\r\n" +
		"--BOUNDARY\r\n" +
		"Content-Disposition: attachment; filename=\"note.bin\"\r\n" +
		"\r\n" +
		"hello\r\n" +
		"--BOUNDARY--\r\n"
	_, _, _, att, err := parseOutboundMail([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(att) != 1 {
		t.Fatalf("want 1 attachment, got %v", att)
	}
	m, ok := att[0].(map[string]any)
	if !ok || m["filename"] != "note.bin" {
		t.Fatalf("bad map: %v", att[0])
	}
}
