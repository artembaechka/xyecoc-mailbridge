package remote

import (
	"testing"
)

func TestParseMailDetailFromViewBody_dataObject(t *testing.T) {
	const raw = `{"status":1,"service":"mail","data":{"id":317390,"subject":"s","from_email":"a@b.c","message":"<p>x</p>"}}`
	d, err := ParseMailDetailFromViewBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != 317390 {
		t.Fatalf("id %d", d.ID)
	}
}

func TestParseMailDetailFromViewBody_rootFields(t *testing.T) {
	const raw = `{"status":1,"id":317390,"subject":"","from_email":"a@b.c","message":"<html></html>"}`
	d, err := ParseMailDetailFromViewBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != 317390 {
		t.Fatalf("id %d", d.ID)
	}
}

func TestParseMailDetailFromViewBody_nestedMail(t *testing.T) {
	const raw = `{"status":1,"data":{"mail":{"id":1,"from_email":"x@y.z","message":"m"}}}`
	d, err := ParseMailDetailFromViewBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != 1 {
		t.Fatalf("id %d", d.ID)
	}
}

func TestParseMailDetailFromViewBody_mailPlusSiblingAttachments(t *testing.T) {
	const raw = `{"status":1,"data":{"mail":{"id":99,"from_email":"a@b.c"},"attachments":[{"id":5,"file_name":"f.jpg","extension":"jpg","created_at":"2026-04-15T12:00:00Z"}]}}`
	d, err := ParseMailDetailFromViewBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Attachments) != 1 || d.Attachments[0].ID != 5 {
		t.Fatalf("attachments %+v", d.Attachments)
	}
}
