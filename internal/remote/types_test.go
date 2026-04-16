package remote

import (
	"encoding/json"
	"testing"
)

func TestTotalCount_unmarshalStringCount(t *testing.T) {
	const raw = `{"count":"2"}`
	var tc TotalCount
	if err := json.Unmarshal([]byte(raw), &tc); err != nil {
		t.Fatal(err)
	}
	if tc.Count != 2 {
		t.Fatalf("got %d want 2", tc.Count)
	}
}

func TestTotalCount_unmarshalNumber(t *testing.T) {
	const raw = `{"count":17}`
	var tc TotalCount
	if err := json.Unmarshal([]byte(raw), &tc); err != nil {
		t.Fatal(err)
	}
	if tc.Count != 17 {
		t.Fatalf("got %d want 17", tc.Count)
	}
}

func TestAPIStatus_unmarshalString(t *testing.T) {
	var r Response
	if err := json.Unmarshal([]byte(`{"status":"1","message":""}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Status != 1 {
		t.Fatalf("got %d want 1", r.Status)
	}
}

func TestMailMutationOK_rootStatus(t *testing.T) {
	var r Response
	if err := json.Unmarshal([]byte(`{"status":1,"message":"success"}`), &r); err != nil {
		t.Fatal(err)
	}
	if !MailMutationOK(&r) {
		t.Fatal("expected OK")
	}
}

func TestMailMutationOK_messageOnly(t *testing.T) {
	var r Response
	if err := json.Unmarshal([]byte(`{"status":0,"message":"success","data":{}}`), &r); err != nil {
		t.Fatal(err)
	}
	if !MailMutationOK(&r) {
		t.Fatal("expected OK when message is success")
	}
}

func TestMailMutationOK_nestedDataStatus(t *testing.T) {
	var r Response
	if err := json.Unmarshal([]byte(`{"status":0,"data":{"status":1,"id":99}}`), &r); err != nil {
		t.Fatal(err)
	}
	if !MailMutationOK(&r) {
		t.Fatal("expected OK when data.status is 1")
	}
}

func TestMailListResponseOK_omittedStatusWithMails(t *testing.T) {
	raw := `{"action":"default","mails":[{"id":1,"read":false}],"total":{"count":"1"}}`
	var r Response
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	_ = MergeMailListFields(&r)
	if !MailListResponseOK(&r) {
		t.Fatal("expected OK when mails present but root status omitted")
	}
}
