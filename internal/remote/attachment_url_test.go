package remote

import (
	"strings"
	"testing"
)

func TestAttachmentURL_usesAPIPathAndExt(t *testing.T) {
	a := Attachment{
		ID:        114402,
		FileName:  "images.jpg",
		Extension: "jpg",
		CreatedAt: "2026-04-15T15:43:00.362089",
	}
	u := AttachmentURL("https://api.example.com", a, "abc", "")
	if !strings.Contains(u, "https://api.example.com/data/attachments/mails/2026-04-15/114402.jpg") {
		t.Fatalf("url: %s", u)
	}
	if !strings.Contains(u, "token=abc") {
		t.Fatalf("token param: %s", u)
	}
}

func TestAttachmentURL_extFromFileName(t *testing.T) {
	a := Attachment{ID: 1, FileName: "x.PNG", Extension: ""}
	u := AttachmentURL("https://api.example.com", a, "t", "")
	if !strings.Contains(u, "/1.png?") {
		t.Fatalf("url: %s", u)
	}
}

func TestAttachmentPathDay_fallbackToMail(t *testing.T) {
	if g := attachmentPathDay("", "2026-04-15T12:00:00Z"); g != "2026-04-15" {
		t.Fatalf("got %q", g)
	}
}

func TestAttachmentURLCandidates_secondDayWhenMailDiffers(t *testing.T) {
	a := Attachment{
		ID:        1,
		FileName:  "x.bin",
		Extension: "bin",
		// attachment dated 16th UTC
		CreatedAt: "2026-04-16T01:00:00Z",
	}
	// mail dated 15th — storage may use mail folder day
	mailCreated := "2026-04-15T22:00:00Z"
	urls := AttachmentURLCandidates("https://cdn.example.com", a, "tok", mailCreated, 99)
	var has15, has16 bool
	for _, u := range urls {
		if containsAll(u, "/data/attachments/mails/2026-04-15/", "/1.bin") {
			has15 = true
		}
		if containsAll(u, "/data/attachments/mails/2026-04-16/", "/1.bin") {
			has16 = true
		}
	}
	if !has15 || !has16 {
		t.Fatalf("expected both day folders, got %#v", urls)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
