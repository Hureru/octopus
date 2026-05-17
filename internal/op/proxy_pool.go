package op

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/cache"
	"golang.org/x/net/proxy"
)

const defaultProxyTestURL = "https://api.openai.com/v1/models"

var proxyConfigurationCache = cache.New[int, model.ProxyConfiguration](16)

func ProxyConfigurationList(ctx context.Context) ([]model.ProxyConfiguration, error) {
	var items []model.ProxyConfiguration
	if err := db.GetDB().WithContext(ctx).Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	counts, err := ProxyConfigurationReferenceCounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].ReferenceCount = counts[items[i].ID]
	}
	return items, nil
}

func ProxyConfigurationGet(id int, ctx context.Context) (*model.ProxyConfiguration, error) {
	var item model.ProxyConfiguration
	if err := db.GetDB().WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func ProxyConfigurationCreate(item *model.ProxyConfiguration, ctx context.Context) error {
	if item == nil {
		return fmt.Errorf("proxy configuration is nil")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	if err := db.GetDB().WithContext(ctx).Create(item).Error; err != nil {
		return err
	}
	proxyConfigurationCache.Set(item.ID, *item)
	return nil
}

func ProxyConfigurationUpdate(req *model.ProxyConfigurationUpdateRequest, ctx context.Context) (*model.ProxyConfiguration, error) {
	if req == nil {
		return nil, fmt.Errorf("proxy update request is nil")
	}
	var existing model.ProxyConfiguration
	if err := db.GetDB().WithContext(ctx).First(&existing, req.ID).Error; err != nil {
		return nil, fmt.Errorf("proxy configuration not found")
	}
	merged := existing
	var selectFields []string
	updates := model.ProxyConfiguration{ID: req.ID}
	if req.Name != nil {
		merged.Name = *req.Name
		selectFields = append(selectFields, "name")
	}
	if req.URL != nil {
		merged.URL = *req.URL
		selectFields = append(selectFields, "url")
	}
	if req.Enabled != nil {
		merged.Enabled = *req.Enabled
		selectFields = append(selectFields, "enabled")
	}
	if req.Remark != nil {
		merged.Remark = *req.Remark
		selectFields = append(selectFields, "remark")
	}
	if len(selectFields) > 0 {
		if err := merged.Validate(); err != nil {
			return nil, err
		}
	}
	if req.Name != nil {
		updates.Name = merged.Name
	}
	if req.URL != nil {
		updates.URL = merged.URL
	}
	if req.Enabled != nil {
		updates.Enabled = merged.Enabled
	}
	if req.Remark != nil {
		updates.Remark = merged.Remark
	}
	if len(selectFields) > 0 {
		if err := db.GetDB().WithContext(ctx).Model(&model.ProxyConfiguration{}).Where("id = ?", req.ID).Select(selectFields).Updates(&updates).Error; err != nil {
			return nil, fmt.Errorf("failed to update proxy configuration: %w", err)
		}
	}
	item, err := ProxyConfigurationGet(req.ID, ctx)
	if err != nil {
		return nil, err
	}
	proxyConfigurationCache.Set(item.ID, *item)
	return item, nil
}

func ProxyConfigurationDelete(id int, ctx context.Context) error {
	if _, err := ProxyConfigurationGet(id, ctx); err != nil {
		return fmt.Errorf("proxy configuration not found")
	}
	count, err := ProxyConfigurationReferenceCount(id, ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("proxy configuration is still referenced")
	}
	if err := db.GetDB().WithContext(ctx).Delete(&model.ProxyConfiguration{}, id).Error; err != nil {
		return err
	}
	proxyConfigurationCache.Del(id)
	return nil
}

func ProxyConfigurationReferenceCount(id int, ctx context.Context) (int, error) {
	counts, err := ProxyConfigurationReferenceCounts(ctx)
	if err != nil {
		return 0, err
	}
	return counts[id], nil
}

func ProxyConfigurationReferences(id int, ctx context.Context) ([]model.ProxyConfigurationReference, error) {
	if _, err := ProxyConfigurationGet(id, ctx); err != nil {
		return nil, fmt.Errorf("proxy configuration not found")
	}

	refs := make([]model.ProxyConfigurationReference, 0)

	var sites []model.Site
	if err := db.GetDB().WithContext(ctx).
		Where("proxy_mode = ? AND proxy_config_id = ?", model.ProxyUsageModePool, id).
		Order("id ASC").Find(&sites).Error; err != nil {
		return nil, err
	}
	for _, site := range sites {
		refs = append(refs, model.ProxyConfigurationReference{
			Type:         model.ProxyConfigurationReferenceTypeSite,
			SiteID:       site.ID,
			SiteName:     site.Name,
			SiteArchived: site.Archived,
		})
	}

	type accountRefRow struct {
		ID       int
		Name     string
		SiteID   int
		SiteName string
		Archived bool
	}
	var accountRows []accountRefRow
	if err := db.GetDB().WithContext(ctx).
		Table("site_accounts").
		Select("site_accounts.id, site_accounts.name, site_accounts.site_id, sites.name as site_name, sites.archived").
		Joins("LEFT JOIN sites ON sites.id = site_accounts.site_id").
		Where("site_accounts.proxy_mode = ? AND site_accounts.proxy_config_id = ?", model.ProxyUsageModePool, id).
		Order("site_accounts.id ASC").Scan(&accountRows).Error; err != nil {
		return nil, err
	}
	for _, row := range accountRows {
		refs = append(refs, model.ProxyConfigurationReference{
			Type:            model.ProxyConfigurationReferenceTypeSiteAccount,
			SiteID:          row.SiteID,
			SiteName:        row.SiteName,
			SiteArchived:    row.Archived,
			SiteAccountID:   row.ID,
			SiteAccountName: row.Name,
		})
	}

	var channels []model.Channel
	if err := db.GetDB().WithContext(ctx).
		Where("proxy_mode = ? AND proxy_config_id = ?", model.ProxyUsageModePool, id).
		Order("id ASC").Find(&channels).Error; err != nil {
		return nil, err
	}
	channelIDs := make([]int, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
	}
	bindingMap, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return nil, err
	}
	for _, channel := range channels {
		ref := model.ProxyConfigurationReference{
			Type:        model.ProxyConfigurationReferenceTypeChannel,
			ChannelID:   channel.ID,
			ChannelName: channel.Name,
		}
		if binding, ok := bindingMap[channel.ID]; ok {
			ref.Type = model.ProxyConfigurationReferenceTypeManagedChannel
			ref.Managed = true
			ref.SiteID = binding.SiteID
			ref.SiteAccountID = binding.SiteAccountID
			ref.ManagedSource = &model.ManagedChannelSource{
				SiteID:          binding.SiteID,
				SiteAccountID:   binding.SiteAccountID,
				SiteUserGroupID: binding.SiteUserGroupID,
				GroupKey:        binding.GroupKey,
			}
		}
		refs = append(refs, ref)
	}

	return refs, nil
}

func ProxyConfigurationReferenceCounts(ctx context.Context) (map[int]int, error) {
	counts := make(map[int]int)
	if err := countProxyReferences(ctx, model.Site{}, counts); err != nil {
		return nil, err
	}
	if err := countProxyReferences(ctx, model.SiteAccount{}, counts); err != nil {
		return nil, err
	}
	if err := countManualChannelProxyReferences(ctx, counts); err != nil {
		return nil, err
	}
	return counts, nil
}

func countProxyReferences(ctx context.Context, table any, counts map[int]int) error {
	type row struct {
		ProxyConfigID int
		Count         int
	}
	var rows []row
	if err := db.GetDB().WithContext(ctx).Model(table).
		Select("proxy_config_id, count(*) as count").
		Where("proxy_mode = ? AND proxy_config_id IS NOT NULL", model.ProxyUsageModePool).
		Group("proxy_config_id").Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		counts[r.ProxyConfigID] += r.Count
	}
	return nil
}

func countManualChannelProxyReferences(ctx context.Context, counts map[int]int) error {
	type row struct {
		ProxyConfigID int
		Count         int
	}
	var rows []row
	if err := db.GetDB().WithContext(ctx).Table("channels").
		Select("channels.proxy_config_id, count(*) as count").
		Where("channels.proxy_mode = ? AND channels.proxy_config_id IS NOT NULL", model.ProxyUsageModePool).
		Where("NOT EXISTS (SELECT 1 FROM site_channel_bindings WHERE site_channel_bindings.channel_id = channels.id)").
		Group("channels.proxy_config_id").Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		counts[r.ProxyConfigID] += r.Count
	}
	return nil
}

func ProxyURLForConfig(id int, ctx context.Context) (string, error) {
	if cached, ok := proxyConfigurationCache.Get(id); ok {
		if !cached.Enabled {
			return "", fmt.Errorf("proxy configuration is disabled")
		}
		return cached.URL, nil
	}
	item, err := ProxyConfigurationGet(id, ctx)
	if err != nil {
		proxyConfigurationCache.Del(id)
		return "", fmt.Errorf("proxy configuration not found")
	}
	proxyConfigurationCache.Set(item.ID, *item)
	if !item.Enabled {
		return "", fmt.Errorf("proxy configuration is disabled")
	}
	return item.URL, nil
}

func proxyConfigurationRefreshCache(ctx context.Context) error {
	var items []model.ProxyConfiguration
	if err := db.GetDB().WithContext(ctx).Find(&items).Error; err != nil {
		return err
	}
	proxyConfigurationCache.Clear()
	for _, item := range items {
		proxyConfigurationCache.Set(item.ID, item)
	}
	return nil
}

func proxyTestTargetHostSafe(parsedTarget *url.URL) error {
	if parsedTarget == nil {
		return fmt.Errorf("test url is required")
	}
	host := strings.TrimSpace(parsedTarget.Hostname())
	if host == "" {
		host = strings.TrimSpace(parsedTarget.Host)
	}
	host = strings.Trim(strings.ToLower(host), ".")
	if host == "" {
		return fmt.Errorf("test url must have a host")
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return fmt.Errorf("test url host is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if proxyTestIPDisallowed(ip) {
			return fmt.Errorf("test url host is not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve test url host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("test url host did not resolve")
	}
	for _, ip := range ips {
		if proxyTestIPDisallowed(ip) {
			return fmt.Errorf("test url host resolves to a disallowed address")
		}
	}
	return nil
}

func proxyTestIPDisallowed(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 169 && v4[1] == 254
	}
	return ip.IsPrivate()
}

func newProxyTestHTTPClient(proxyURLStr string) (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}
	cloned := transport.Clone()
	proxyURL, err := url.Parse(proxyURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}
	switch proxyURL.Scheme {
	case "http", "https":
		cloned.Proxy = http.ProxyURL(proxyURL)
	case "socks", "socks5":
		socksDialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("invalid socks proxy: %w", err)
		}
		cloned.Proxy = nil
		cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return socksDialer.Dial(network, addr)
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
	return &http.Client{Transport: cloned}, nil
}

func ProxyConfigurationTest(req model.ProxyTestRequest, ctx context.Context) (model.ProxyTestResult, error) {
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" {
		targetURL = defaultProxyTestURL
	}
	parsedTarget, err := url.Parse(targetURL)
	if err != nil || parsedTarget.Scheme == "" || parsedTarget.Host == "" || (parsedTarget.Scheme != "http" && parsedTarget.Scheme != "https") {
		return model.ProxyTestResult{Success: false, Message: "test url must be a valid http or https url"}, nil
	}
	if err := proxyTestTargetHostSafe(parsedTarget); err != nil {
		return model.ProxyTestResult{Success: false, Message: err.Error()}, nil
	}

	proxyURL := strings.TrimSpace(req.ProxyURL)
	if req.ProxyConfigID != nil && *req.ProxyConfigID > 0 {
		item, getErr := ProxyConfigurationGet(*req.ProxyConfigID, ctx)
		if getErr != nil {
			return model.ProxyTestResult{Success: false, Message: "proxy configuration not found"}, nil
		}
		if !item.Enabled {
			return model.ProxyTestResult{Success: false, Message: "proxy configuration is disabled"}, nil
		}
		proxyURL = item.URL
	}
	if proxyURL == "" {
		return model.ProxyTestResult{Success: false, Message: "proxy url is required"}, nil
	}
	normalizedProxyURL, err := model.NormalizeProxyURL(proxyURL)
	if err != nil {
		return model.ProxyTestResult{Success: false, Message: err.Error()}, nil
	}

	httpClient, err := newProxyTestHTTPClient(normalizedProxyURL)
	if err != nil {
		return model.ProxyTestResult{Success: false, Message: err.Error()}, nil
	}
	httpClient.Timeout = 20 * time.Second

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return model.ProxyTestResult{Success: false, Message: err.Error()}, nil
	}
	httpReq.Header.Set("User-Agent", "Octopus Proxy Pool Tester")
	resp, err := httpClient.Do(httpReq)
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		return model.ProxyTestResult{Success: false, DurationMS: durationMS, Message: err.Error()}, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return model.ProxyTestResult{Success: true, StatusCode: resp.StatusCode, DurationMS: durationMS, Message: "proxy is reachable"}, nil
}
