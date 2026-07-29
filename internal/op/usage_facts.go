package op

import (
	"context"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	usageFactBatchSize = 200
	usageFactQueueSize = 5000
)

type usagePendingRecord struct {
	request  model.UsageRequestFact
	attempts []model.UsageAttemptFact
}

type usageChannelDimension struct {
	ChannelID       int
	SiteID          int
	SiteName        string
	SiteAccountID   int
	SiteAccountName string
}

var (
	usagePending     = make([]usagePendingRecord, 0, usageFactBatchSize)
	usagePendingLock sync.Mutex
	usageFlushLock   sync.Mutex
)

func usageFactsSnapshot(ctx context.Context, relayLog model.RelayLog) (usagePendingRecord, error) {
	record := usageFactsFromRelayLog(relayLog)
	batch := []usagePendingRecord{record}
	if err := enrichUsageDimensions(ctx, batch); err != nil {
		return usagePendingRecord{}, err
	}
	return batch[0], nil
}

func enqueueUsageFacts(ctx context.Context, record usagePendingRecord) error {
	for {
		usagePendingLock.Lock()
		if len(usagePending) < usageFactQueueSize {
			usagePending = append(usagePending, record)
			shouldFlush := len(usagePending) >= usageFactBatchSize
			usagePendingLock.Unlock()
			if shouldFlush {
				signalRelayLogFlush()
			}
			return nil
		}
		usagePendingLock.Unlock()

		// Apply backpressure instead of dropping billing facts. The request may
		// already be canceled, so RelayLogAdd passes a detached persistence
		// context here.
		if err := usageFactsFlushPendingBatch(ctx, usageFactBatchSize); err != nil {
			return err
		}
	}
}

func usageFactsFromRelayLog(relayLog model.RelayLog) usagePendingRecord {
	tokenSource := relayLog.TokenSource
	if tokenSource == "" {
		tokenSource = model.UsageValueSourceUnknown
	}
	priceConvertible := relayLogPriceConvertible(relayLog)
	effectiveInputTokens := int64(relayLog.InputTokens)
	if relayLog.BillInputTokens != nil {
		effectiveInputTokens = nonNegativeIntPointerValue(relayLog.BillInputTokens) +
			nonNegativeIntPointerValue(relayLog.CacheReadTokens) +
			nonNegativeIntPointerValue(relayLog.CacheWriteTokens)
	}
	snapshotCount := 0
	snapshotInputTokens := int64(0)
	snapshotOutputTokens := int64(0)
	snapshotCacheReadTokens := int64(0)
	snapshotCacheWriteTokens := int64(0)
	snapshotCostUSD := float64(0)
	snapshotTokenSource := model.UsageValueSource("")
	for i := range relayLog.Attempts {
		if usage := relayLog.Attempts[i].Usage; usage != nil {
			snapshotCount++
			snapshotInputTokens += usage.InputTokens
			snapshotOutputTokens += usage.OutputTokens
			snapshotCacheReadTokens += usage.CacheReadTokens
			snapshotCacheWriteTokens += usage.CacheWriteTokens
			snapshotCostUSD += usage.CostUSD
			if snapshotTokenSource == "" {
				snapshotTokenSource = usage.TokenSource
			} else if usage.TokenSource != "" && usage.TokenSource != snapshotTokenSource {
				snapshotTokenSource = model.UsageValueSourceUnknown
			}
		}
	}
	if snapshotCount > 0 {
		effectiveInputTokens = snapshotInputTokens
		if tokenSource == model.UsageValueSourceUnknown && snapshotTokenSource != "" {
			tokenSource = snapshotTokenSource
		}
	}
	request := model.UsageRequestFact{
		RelayLogID:        relayLog.ID,
		Time:              relayLog.Time,
		ChannelID:         relayLog.ChannelId,
		ChannelName:       relayLog.ChannelName,
		APIKeyID:          relayLog.RequestAPIKeyID,
		APIKeyName:        relayLog.RequestAPIKeyName,
		RouteCandidateID:  relayLog.RouteCandidateID,
		RequestModel:      relayLog.RequestModelName,
		ActualModel:       relayLog.ActualModelName,
		CanonicalModel:    relayLog.CanonicalModelName,
		Outcome:           relayLog.Outcome,
		InputTokens:       effectiveInputTokens,
		OutputTokens:      int64(relayLog.OutputTokens),
		CacheReadTokens:   intPointerValue(relayLog.CacheReadTokens),
		CacheWriteTokens:  intPointerValue(relayLog.CacheWriteTokens),
		CostUSD:           relayLog.Cost,
		DurationMS:        int64(relayLog.UseTime),
		FTUTMS:            int64(relayLog.Ftut),
		FTUTKnown:         relayLog.Ftut > 0,
		TokenSource:       tokenSource,
		PriceQuoteID:      relayLog.PriceQuoteID,
		PriceSource:       relayLog.PriceSource,
		PriceUnit:         relayLog.PriceUnit,
		PriceCurrency:     relayLog.PriceCurrency,
		PriceInput:        relayLog.PriceInput,
		PriceOutput:       relayLog.PriceOutput,
		PriceCacheRead:    relayLog.PriceCacheRead,
		PriceCacheWrite:   relayLog.PriceCacheWrite,
		PricePerRequest:   relayLog.PricePerRequest,
		PriceMultiplier:   relayLog.PriceGroupMultiplier,
		PriceRateToUSD:    relayLog.PriceExchangeRateUSD,
		PriceObservedAt:   relayLog.PriceObservedAt,
		PriceStale:        relayLog.PriceStale,
		PriceConvertible:  priceConvertible,
		PriceOriginalCost: relayLog.PriceOriginalCost,
		PriceMatchReason:  relayLog.PriceMatchReason,
		InboundProtocol:   relayLog.InboundProtocol,
		OutboundProtocol:  relayLog.OutboundProtocol,
		ProtocolMode:      relayLog.ProtocolMode,
	}
	if snapshotCount > 0 {
		request.OutputTokens = snapshotOutputTokens
		request.CacheReadTokens = snapshotCacheReadTokens
		request.CacheWriteTokens = snapshotCacheWriteTokens
		request.CostUSD = snapshotCostUSD
	}

	attempts := make([]model.UsageAttemptFact, 0, len(relayLog.Attempts))
	usageIndex := -1
	if snapshotCount == 0 {
		usageIndex = usageAttributionAttemptIndex(relayLog)
	}
	for i, attempt := range relayLog.Attempts {
		if attempt.Status != model.AttemptSuccess &&
			attempt.Status != model.AttemptFailed &&
			attempt.Status != model.AttemptCanceled {
			continue
		}
		outcome := attempt.Outcome
		if outcome == "" {
			outcome = outcomeForAttemptStatus(attempt.Status)
		}
		attribution := attempt.Attribution
		if attribution == "" && outcome == model.RequestOutcomeFailed {
			attribution = model.AttemptAttributionUpstream
		}
		actualModel := strings.TrimSpace(attempt.ModelName)
		if actualModel == "" {
			actualModel = relayLog.ActualModelName
		}
		fact := model.UsageAttemptFact{
			RelayLogID:       relayLog.ID,
			AttemptNumber:    attempt.AttemptNum,
			Time:             relayLog.Time,
			ChannelID:        attempt.ChannelID,
			ChannelName:      attempt.ChannelName,
			RouteCandidateID: attempt.RouteCandidateID,
			APIKeyID:         relayLog.RequestAPIKeyID,
			APIKeyName:       relayLog.RequestAPIKeyName,
			RequestModel:     relayLog.RequestModelName,
			ActualModel:      actualModel,
			CanonicalModel:   relayLog.CanonicalModelName,
			UpstreamModel:    attempt.ModelName,
			Status:           attempt.Status,
			StatusCode:       attempt.StatusCode,
			Outcome:          outcome,
			Attribution:      attribution,
			DurationMS:       int64(attempt.Duration),
			TokenSource:      model.UsageValueSourceUnknown,
			InboundProtocol:  relayLog.InboundProtocol,
			OutboundProtocol: relayLog.OutboundProtocol,
			ProtocolMode:     relayLog.ProtocolMode,
		}
		if attempt.Usage != nil {
			applyAttemptUsageSnapshot(&fact, *attempt.Usage)
		} else if i == usageIndex {
			if fact.RouteCandidateID == 0 {
				fact.RouteCandidateID = relayLog.RouteCandidateID
			}
			fact.InputTokens = request.InputTokens
			fact.OutputTokens = request.OutputTokens
			fact.CacheReadTokens = request.CacheReadTokens
			fact.CacheWriteTokens = request.CacheWriteTokens
			fact.CostUSD = request.CostUSD
			fact.FTUTMS = request.FTUTMS
			fact.FTUTKnown = request.FTUTKnown
			fact.UsageAttributed = true
			fact.TokenSource = tokenSource
			fact.PriceQuoteID = relayLog.PriceQuoteID
			fact.PriceSource = relayLog.PriceSource
			fact.PriceUnit = relayLog.PriceUnit
			fact.PriceCurrency = relayLog.PriceCurrency
			fact.PriceInput = relayLog.PriceInput
			fact.PriceOutput = relayLog.PriceOutput
			fact.PriceCacheRead = relayLog.PriceCacheRead
			fact.PriceCacheWrite = relayLog.PriceCacheWrite
			fact.PricePerRequest = relayLog.PricePerRequest
			fact.PriceMultiplier = relayLog.PriceGroupMultiplier
			fact.PriceRateToUSD = relayLog.PriceExchangeRateUSD
			fact.PriceObservedAt = relayLog.PriceObservedAt
			fact.PriceStale = relayLog.PriceStale
			fact.PriceConvertible = priceConvertible
			fact.PriceOriginalCost = relayLog.PriceOriginalCost
			fact.PriceMatchReason = relayLog.PriceMatchReason
		}
		attempts = append(attempts, fact)
	}
	return usagePendingRecord{request: request, attempts: attempts}
}

func relayLogPriceConvertible(relayLog model.RelayLog) bool {
	if relayLog.PriceConvertible {
		return true
	}
	if relayLog.PriceSource == "" || relayLog.PriceSource == model.PriceQuoteSourceUnknown {
		return false
	}
	return relayLog.PriceExchangeRateUSD > 0 || strings.EqualFold(relayLog.PriceCurrency, "USD")
}

func outcomeForAttemptStatus(status model.AttemptStatus) model.RequestOutcome {
	switch status {
	case model.AttemptSuccess:
		return model.RequestOutcomeSuccess
	case model.AttemptCanceled:
		return model.RequestOutcomeClientCanceled
	default:
		return model.RequestOutcomeFailed
	}
}

func usageAttributionAttemptIndex(relayLog model.RelayLog) int {
	attempts := relayLog.Attempts
	if relayLog.RouteCandidateID > 0 {
		for i := len(attempts) - 1; i >= 0; i-- {
			if attempts[i].RouteCandidateID == relayLog.RouteCandidateID &&
				isUsageAttributionStatus(attempts[i].Status) {
				return i
			}
		}
	}
	if relayLog.ChannelId > 0 {
		for i := len(attempts) - 1; i >= 0; i-- {
			if attempts[i].ChannelID == relayLog.ChannelId &&
				isUsageAttributionStatus(attempts[i].Status) {
				return i
			}
		}
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].Status == model.AttemptSuccess || attempts[i].Status == model.AttemptCanceled {
			return i
		}
	}
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].Status == model.AttemptFailed {
			return i
		}
	}
	return -1
}

func isUsageAttributionStatus(status model.AttemptStatus) bool {
	return status == model.AttemptSuccess ||
		status == model.AttemptCanceled ||
		status == model.AttemptFailed
}

func applyAttemptUsageSnapshot(fact *model.UsageAttemptFact, usage model.AttemptUsageSnapshot) {
	fact.InputTokens = usage.InputTokens
	fact.OutputTokens = usage.OutputTokens
	fact.CacheReadTokens = usage.CacheReadTokens
	fact.CacheWriteTokens = usage.CacheWriteTokens
	fact.CostUSD = usage.CostUSD
	fact.FTUTMS = usage.FTUTMS
	fact.FTUTKnown = usage.FTUTKnown
	fact.UsageAttributed = true
	fact.TokenSource = usage.TokenSource
	if fact.TokenSource == "" {
		fact.TokenSource = model.UsageValueSourceUnknown
	}
	fact.PriceQuoteID = usage.PriceQuoteID
	fact.PriceSource = usage.PriceSource
	fact.PriceUnit = usage.PriceUnit
	fact.PriceCurrency = usage.PriceCurrency
	fact.PriceInput = usage.PriceInput
	fact.PriceOutput = usage.PriceOutput
	fact.PriceCacheRead = usage.PriceCacheRead
	fact.PriceCacheWrite = usage.PriceCacheWrite
	fact.PricePerRequest = usage.PricePerRequest
	fact.PriceMultiplier = usage.PriceMultiplier
	fact.PriceRateToUSD = usage.PriceRateToUSD
	fact.PriceObservedAt = usage.PriceObservedAt
	fact.PriceStale = usage.PriceStale
	fact.PriceConvertible = usage.PriceConvertible
	fact.PriceOriginalCost = usage.PriceOriginalCost
	fact.PriceMatchReason = usage.PriceMatchReason
}

func intPointerValue(value *int) int64 {
	if value == nil {
		return 0
	}
	return int64(*value)
}

func nonNegativeIntPointerValue(value *int) int64 {
	return max(0, intPointerValue(value))
}

func UsageFactsPendingLen() int {
	usagePendingLock.Lock()
	defer usagePendingLock.Unlock()
	return len(usagePending)
}

func usageFactsDrainPending(ctx context.Context, maxBatches int) error {
	if maxBatches <= 0 {
		maxBatches = 1
	}
	for i := 0; i < maxBatches; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if UsageFactsPendingLen() == 0 {
			return nil
		}
		if err := usageFactsFlushPendingBatch(ctx, usageFactBatchSize); err != nil {
			return err
		}
	}
	if UsageFactsPendingLen() > 0 {
		signalRelayLogFlush()
	}
	return nil
}

func UsageFactsFlushPending(ctx context.Context) error {
	for UsageFactsPendingLen() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := usageFactsFlushPendingBatch(ctx, usageFactBatchSize); err != nil {
			return err
		}
	}
	return nil
}

func usageFactsFlushPendingBatch(ctx context.Context, batchSize int) error {
	usageFlushLock.Lock()
	defer usageFlushLock.Unlock()

	usagePendingLock.Lock()
	if len(usagePending) == 0 {
		usagePendingLock.Unlock()
		return nil
	}
	if batchSize <= 0 || batchSize > len(usagePending) {
		batchSize = len(usagePending)
	}
	batch := make([]usagePendingRecord, batchSize)
	copy(batch, usagePending[:batchSize])
	usagePendingLock.Unlock()

	if err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return persistUsageRecords(tx, batch)
	}); err != nil {
		return err
	}

	flushed := make(map[int64]struct{}, len(batch))
	for _, item := range batch {
		flushed[item.request.RelayLogID] = struct{}{}
	}
	usagePendingLock.Lock()
	kept := usagePending[:0]
	for _, item := range usagePending {
		if _, ok := flushed[item.request.RelayLogID]; !ok {
			kept = append(kept, item)
		}
	}
	usagePending = kept
	if len(usagePending) == 0 {
		usagePending = make([]usagePendingRecord, 0, usageFactBatchSize)
	}
	usagePendingLock.Unlock()
	return nil
}

func persistUsageRecords(tx *gorm.DB, records []usagePendingRecord) error {
	if len(records) == 0 {
		return nil
	}
	requests := make([]model.UsageRequestFact, 0, len(records))
	attempts := make([]model.UsageAttemptFact, 0, len(records)*2)
	for i := range records {
		requests = append(requests, records[i].request)
		attempts = append(attempts, records[i].attempts...)
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(&requests, usageFactBatchSize).Error; err != nil {
		return err
	}
	if len(attempts) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(&attempts, usageFactBatchSize).Error
}

func enrichUsageDimensions(ctx context.Context, batch []usagePendingRecord) error {
	channelIDs := make([]int, 0)
	seen := make(map[int]struct{})
	for _, record := range batch {
		if record.request.ChannelID > 0 {
			seen[record.request.ChannelID] = struct{}{}
		}
		for _, attempt := range record.attempts {
			if attempt.ChannelID > 0 {
				seen[attempt.ChannelID] = struct{}{}
			}
		}
	}
	for id := range seen {
		channelIDs = append(channelIDs, id)
	}
	if len(channelIDs) == 0 {
		return nil
	}

	var rows []usageChannelDimension
	if err := db.GetDB().WithContext(ctx).
		Table("site_channel_bindings AS binding").
		Select(
			"binding.channel_id, binding.site_id, site.name AS site_name, "+
				"binding.site_account_id, account.name AS site_account_name",
		).
		Joins("LEFT JOIN sites AS site ON site.id = binding.site_id").
		Joins("LEFT JOIN site_accounts AS account ON account.id = binding.site_account_id").
		Where("binding.channel_id IN ?", channelIDs).
		Scan(&rows).Error; err != nil {
		return err
	}
	byChannel := make(map[int]usageChannelDimension, len(rows))
	for _, row := range rows {
		byChannel[row.ChannelID] = row
	}
	for i := range batch {
		applyUsageDimensionToRequest(&batch[i].request, byChannel[batch[i].request.ChannelID])
		for j := range batch[i].attempts {
			applyUsageDimensionToAttempt(&batch[i].attempts[j], byChannel[batch[i].attempts[j].ChannelID])
		}
	}
	return nil
}

func applyUsageDimensionToRequest(fact *model.UsageRequestFact, dimension usageChannelDimension) {
	fact.SiteID = dimension.SiteID
	fact.SiteName = dimension.SiteName
	fact.SiteAccountID = dimension.SiteAccountID
	fact.SiteAccountName = dimension.SiteAccountName
}

func applyUsageDimensionToAttempt(fact *model.UsageAttemptFact, dimension usageChannelDimension) {
	fact.SiteID = dimension.SiteID
	fact.SiteName = dimension.SiteName
	fact.SiteAccountID = dimension.SiteAccountID
	fact.SiteAccountName = dimension.SiteAccountName
}

func usageFactsResetForTest() {
	usagePendingLock.Lock()
	usagePending = make([]usagePendingRecord, 0, usageFactBatchSize)
	usagePendingLock.Unlock()
}
