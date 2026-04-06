package balancer

import (
	"testing"
	"time"
)

func TestDeleteStickyRemovesSession(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 20)
	if entry := GetSticky(1, "gpt-4o", 60*time.Second); entry == nil || entry.ChannelKeyID != 20 {
		t.Fatalf("expected sticky session to exist before delete, got %#v", entry)
	}

	DeleteSticky(1, "gpt-4o")
	if entry := GetSticky(1, "gpt-4o", 60*time.Second); entry != nil {
		t.Fatalf("expected sticky session to be deleted, got %#v", entry)
	}
}
