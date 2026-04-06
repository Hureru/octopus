package model

import (
	"testing"
	"time"
)

func TestGetChannelKeyPrefersPreferredKeyID(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "first", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2, got %d", selected.ID)
	}
}

func TestGetChannelKeyFallsBackWhenPreferredKeyUnavailable(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "fallback", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 1 {
		t.Fatalf("expected fallback key 1 when preferred key is cooling down, got %d", selected.ID)
	}
}
