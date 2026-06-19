package helper

import (
	"context"
	"net/http"
	"time"
)

func GetUrlDelay(httpClient *http.Client, url string, ctx context.Context) (int, error) {
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	// 防止 Go 默认 User-Agent 泄露到上游
	req.Header.Set("User-Agent", "")
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return int(time.Since(start).Milliseconds()), nil
}
