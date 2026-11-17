package model

import (
	"net/http"
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

func TestGetChannelKeyUsesPreferredKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "fallback", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2 despite recent 429, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesLowestCostKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "recent-429", TotalCost: 1, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
			{ID: 2, Enabled: true, ChannelKey: "other", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey()
	if selected.ID != 1 {
		t.Fatalf("expected lowest cost key 1 despite recent 429, got %d", selected.ID)
	}
}

func TestCustomHeaderResolveValue(t *testing.T) {
	client := http.Header{}
	client.Set("User-Agent", "codex/1.2")
	client.Set("X-Session", "abc")

	cases := []struct {
		name   string
		value  string
		client http.Header
		want   string
		wantOK bool
	}{
		{"literal value is untouched", "static", client, "static", true},
		{"literal value needs no client request", "static", nil, "static", true},
		{"empty literal is kept so a header can still be blanked", "", client, "", true},
		{"placeholder is replaced", "{client_header:User-Agent}", client, "codex/1.2", true},
		{"lookup is case-insensitive and trims the name", "{client_header: x-session }", client, "abc", true},
		{"placeholder inside other text", "ua={client_header:User-Agent};s={client_header:X-Session}", client, "ua=codex/1.2;s=abc", true},
		{"missing client header drops the header", "{client_header:X-Missing}", client, "", false},
		{"no client request drops the header", "{client_header:User-Agent}", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CustomHeader{HeaderKey: "X-Test", HeaderValue: tc.value}.ResolveValue(tc.client)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("ResolveValue(%q) = %q, %v; want %q, %v", tc.value, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
