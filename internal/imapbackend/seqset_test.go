package imapbackend

import (
	"testing"

	"github.com/emersion/go-imap"
)

func TestSeqSetAddressesMessage_UIDFetchStar(t *testing.T) {
	s, err := imap.ParseSeqSet("*")
	if err != nil {
		t.Fatal(err)
	}
	if s.Contains(317390) {
		t.Fatal("go-imap: * should not Contains large UID")
	}
	if !s.Dynamic() {
		t.Fatal("expected dynamic")
	}
	if !seqSetAddressesMessage(true, s, 317390, 2, 317390) {
		t.Fatal("max UID should match *")
	}
	if seqSetAddressesMessage(true, s, 317384, 2, 317390) {
		t.Fatal("non-max UID should not match *")
	}
}

func TestSeqSetAddressesMessage_UIDFetchRange(t *testing.T) {
	s, err := imap.ParseSeqSet("1:*")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Contains(317390) {
		t.Fatal("1:* should contain UID")
	}
}
