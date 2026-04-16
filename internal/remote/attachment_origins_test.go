package remote

import (
	"testing"
	"time"
)

func TestAttachmentHTTPOrigins_CDNFirstThenAPI(t *testing.T) {
	c := NewClient("https://api.example.com", "ru", time.Second)
	c.AssetURL = "https://cdn.example.com"
	o := attachmentHTTPOrigins(c)
	if len(o) != 2 || o[0] != "https://cdn.example.com" || o[1] != "https://api.example.com" {
		t.Fatalf("got %v", o)
	}
}

func TestAttachmentHTTPOrigins_noDuplicateWhenAssetEmpty(t *testing.T) {
	c := NewClient("https://api.example.com", "ru", time.Second)
	o := attachmentHTTPOrigins(c)
	if len(o) != 1 || o[0] != "https://api.example.com" {
		t.Fatalf("got %v", o)
	}
}
