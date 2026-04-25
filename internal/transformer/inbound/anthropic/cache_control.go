package anthropic

import (
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func convertToAnthropicCacheControl(c *model.CacheControl) *CacheControl {
	if c == nil {
		return nil
	}

	sanitized := sanitizeCacheControlPair(c.Type, c.TTL)
	if sanitized.Type == "" {
		return nil
	}
	return &CacheControl{
		Type: sanitized.Type,
		TTL:  sanitized.TTL,
	}
}

func convertToLLMCacheControl(c *CacheControl) *model.CacheControl {
	if c == nil {
		return nil
	}

	return &model.CacheControl{
		Type: c.Type,
		TTL:  c.TTL,
	}
}

// sanitizeCacheControlPair normalises Anthropic cache_control values before emitting Anthropic wire payloads.
// Unknown `type` is dropped; unknown `ttl` is dropped and Anthropic falls back to the provider default.
func sanitizeCacheControlPair(typ, ttl string) struct{ Type, TTL string } {
	out := struct{ Type, TTL string }{Type: typ, TTL: ttl}
	if out.Type != "" && out.Type != model.CacheControlTypeEphemeral {
		log.Warnf("anthropic cache_control: unknown type %q, dropping cache_control before Anthropic emission", out.Type)
		out.Type = ""
		out.TTL = ""
	}
	if out.TTL != "" && out.TTL != model.CacheTTL5m && out.TTL != model.CacheTTL1h {
		log.Warnf("anthropic cache_control: unsupported ttl %q, falling back to provider default", out.TTL)
		out.TTL = ""
	}
	return out
}
