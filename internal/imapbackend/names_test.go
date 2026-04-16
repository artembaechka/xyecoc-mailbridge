package imapbackend

import "testing"

func TestImapMailboxLeaf(t *testing.T) {
	if g := imapMailboxLeaf(`a\b\Tag-9`); g != "Tag-9" {
		t.Fatalf("got %q", g)
	}
	if g := imapMailboxLeaf("Tag-9"); g != "Tag-9" {
		t.Fatalf("flat: got %q", g)
	}
}

func TestFolderNameForAPI(t *testing.T) {
	got, err := folderNameForAPI("  a/b/My Folder  ")
	if err != nil || got != "My Folder" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := folderNameForAPI("INBOX"); err == nil {
		t.Fatal("want error for INBOX")
	}
	if _, err := folderNameForAPI("Trash"); err == nil {
		t.Fatal("want error for Trash")
	}
	if _, err := folderNameForAPI(""); err == nil {
		t.Fatal("want error for empty")
	}
	if g, err := folderNameForAPI("Folder-9"); err != nil || g != "Folder-9" {
		t.Fatalf("folder id style: %q %v", g, err)
	}
}

func TestUniqueTagDisplayName(t *testing.T) {
	occupied := map[string]struct{}{}
	spec := MailboxSpec{Param1: "tags", Param2: "117"}
	if g := uniqueTagDisplayName("123", spec, occupied); g != "Tag-123" {
		t.Fatalf("first tag: got %q", g)
	}
	spec2 := MailboxSpec{Param1: "tags", Param2: "119"}
	if g := uniqueTagDisplayName("123", spec2, occupied); g != "Tag-123 [tags 119]" {
		t.Fatalf("disambiguation: got %q", g)
	}
}

func TestParseMailboxName(t *testing.T) {
	cases := []struct {
		in   string
		p1   string
		p2   string
		want string
	}{
		{"INBOX", "inbox", "", "INBOX"},
		{"Sent", "sent", "", "Sent"},
		{"Folder-12", "folders", "12", "Folder-12"},
		{"Tag-3", "tags", "3", "Tag-3"},
	}
	for _, tc := range cases {
		s, err := ParseMailboxName(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if s.Param1 != tc.p1 || s.Param2 != tc.p2 || CanonicalName(s) != tc.want {
			t.Fatalf("%q got param1=%q param2=%q canon=%q want %q,%q,%q", tc.in, s.Param1, s.Param2, CanonicalName(s), tc.p1, tc.p2, tc.want)
		}
	}
}

func TestPageIDForMailboxSpec(t *testing.T) {
	if g := PageIDForMailboxSpec(MailboxSpec{Param1: "inbox"}); g != "/inbox/" {
		t.Fatalf("inbox: %q", g)
	}
	if g := PageIDForMailboxSpec(MailboxSpec{Param1: "trash"}); g != "/trash/" {
		t.Fatalf("trash: %q", g)
	}
	if g := PageIDForMailboxSpec(MailboxSpec{Param1: "folders", Param2: "91"}); g != "/inbox/folders/91/" {
		t.Fatalf("folder: %q", g)
	}
	if g := PageIDForMailboxSpec(MailboxSpec{Param1: "tags", Param2: "115"}); g != "/inbox/tags/115/" {
		t.Fatalf("tag: %q", g)
	}
}
