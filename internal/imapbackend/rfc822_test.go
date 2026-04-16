package imapbackend

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message"
	"github.com/artembaechka/mailbridge/internal/remote"
)

func TestBuildRFC822_multipartContentType(t *testing.T) {
	d := &remote.MailDetail{
		ID:        1,
		Subject:   "subj",
		FromEmail: "a@b.c",
		To:        "x@y.z",
		FromName:  "A",
		CreatedAt: "2026-04-15T12:00:00Z",
	}
	html := []byte("<p>hi</p>")
	atts := []attachmentPart{
		{fileName: "note.txt", contentType: "text/plain", data: []byte("abc")},
	}
	raw := BuildRFC822(d, html, time.Unix(0, 0).UTC(), atts)
	e, err := message.Read(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mt, _, err := e.Header.ContentType()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(mt, "multipart/") {
		t.Fatalf("expected multipart root, got %q", mt)
	}
}

func TestBuildRFC822_noAttachmentsUsesHTML(t *testing.T) {
	d := &remote.MailDetail{
		ID:        2,
		Subject:   "s",
		FromEmail: "a@b.c",
		To:        "x@y.z",
		CreatedAt: "2026-04-15T12:00:00Z",
	}
	raw := BuildRFC822(d, []byte("<html>x</html>"), time.Unix(0, 0).UTC(), nil)
	if !strings.Contains(string(raw), "Content-Type: text/html") {
		t.Fatalf("expected text/html only message, got prefix %q", string(raw[:min(120, len(raw))]))
	}
}

func TestMimeTypeForAttachment(t *testing.T) {
	ct := mimeTypeForAttachment(remote.Attachment{Extension: "jpg"})
	if !strings.HasPrefix(ct, "image/") {
		t.Fatalf("want image/* got %q", ct)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
