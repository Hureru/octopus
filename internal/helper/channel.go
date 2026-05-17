package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func ChannelHttpClient(channel *model.Channel) (*http.Client, error) {
	return ChannelHTTPClientWithContext(context.Background(), channel)
}

func ChannelHTTPClientWithContext(ctx context.Context, channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	switch channel.ProxyMode {
	case "", model.ProxyUsageModeDirect:
		return client.GetHTTPClientSystemProxy(false)
	case model.ProxyUsageModeSystem:
		return client.GetHTTPClientSystemProxy(true)
	case model.ProxyUsageModePool:
		if channel.ProxyConfigID == nil || *channel.ProxyConfigID <= 0 {
			return nil, fmt.Errorf("proxy config id is required when proxy mode is pool")
		}
		proxyURL, err := op.ProxyURLForConfig(*channel.ProxyConfigID, ctx)
		if err != nil {
			return nil, err
		}
		return client.GetHTTPClientCustomProxy(proxyURL)
	default:
		return nil, fmt.Errorf("unsupported proxy mode: %s", channel.ProxyMode)
	}
}

func ChannelBaseUrlDelayUpdate(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	newBaseUrls := make([]model.BaseUrl, 0, len(channel.BaseUrls))
	for _, baseUrl := range channel.BaseUrls {
		if baseUrl.URL == "" {
			continue
		}
		httpClient, err := ChannelHTTPClientWithContext(ctx, channel)
		if err != nil {
			log.Warnf("failed to get http client (channel=%d): %v", channel.ID, err)
			continue
		}
		delay, err := GetUrlDelay(httpClient, baseUrl.URL, ctx)
		if err != nil {
			log.Warnf("failed to get url delay (channel=%d): %v", channel.ID, err)
			continue
		}
		newBaseUrls = append(newBaseUrls, model.BaseUrl{
			URL:   baseUrl.URL,
			Delay: delay,
		})
	}
	if len(newBaseUrls) > 0 {
		op.ChannelBaseUrlUpdate(channel.ID, newBaseUrls)
	}
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	op.ChannelAutoGroup(channel, ctx)
}
