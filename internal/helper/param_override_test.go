package helper

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMergeParamOverride(t *testing.T) {
	override := func(s string) *string { return &s }
	// 9007199254740993 超出 float64 的精度：没被覆盖的值必须原样保留
	body := `{"model":"gpt-4o","seed":9007199254740993,"tools":[{"type":"custom"}]}`
	for name, tc := range map[string]struct {
		body     string
		override *string
		want     string
	}{
		"no override":            {body, nil, body},
		"blank override":         {body, override("  "), body},
		"override wins":          {body, override(`{"model":"o3","service_tier":"priority"}`), `{"model":"o3","seed":9007199254740993,"service_tier":"priority","tools":[{"type":"custom"}]}`},
		"null override":          {`{"a":1}`, override(`{"a":null}`), `{"a":null}`},
		"override not an object": {body, override(`["x"]`), body},
		"body not an object":     {`["x"]`, override(`{"a":1}`), `["x"]`},
		"body null":              {`null`, override(`{"a":1}`), `null`},
		"body not json":          {`not json`, override(`{"a":1}`), `not json`},
	} {
		got, err := MergeParamOverride([]byte(tc.body), tc.override)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: got %s (err=%v), want %s", name, got, err, tc.want)
		}
	}
}

func TestApplyParamOverrideRewritesTheRequestBody(t *testing.T) {
	override := `{"b":2}`
	request, _ := http.NewRequest(http.MethodPost, "http://upstream.test", strings.NewReader(`{"a":1}`))
	if err := ApplyParamOverride(request, &override); err != nil {
		t.Fatalf("ApplyParamOverride failed: %v", err)
	}
	const want = `{"a":1,"b":2}`
	body, _ := io.ReadAll(request.Body)
	replay, _ := request.GetBody()
	replayed, _ := io.ReadAll(replay)
	if string(body) != want || string(replayed) != want || request.ContentLength != int64(len(want)) {
		t.Fatalf("body=%s replay=%s content-length=%d, want %s", body, replayed, request.ContentLength, want)
	}
}
