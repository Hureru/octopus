package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"gorm.io/gorm"
)

const (
	relayLogRepairRuleV1    = "responses-terminal-v1"
	relayLogRepairBatchSize = 500
)

type RelayLogRepairFilter struct {
	StartTime *int64 `json:"start_time,omitempty"`
	EndTime   *int64 `json:"end_time,omitempty"`
}

type RelayLogRepairSample struct {
	ID           int64  `json:"id"`
	Time         int64  `json:"time"`
	Model        string `json:"model"`
	Channel      string `json:"channel"`
	OutputTokens int    `json:"output_tokens"`
}

type RelayLogRepairPreview struct {
	RuleVersion string                 `json:"rule_version"`
	AuditID     string                 `json:"audit_id"`
	Matched     int                    `json:"matched"`
	Excluded    int                    `json:"excluded"`
	Reasons     map[string]int         `json:"reasons"`
	Samples     []RelayLogRepairSample `json:"samples"`
}

type RelayLogRepairResult struct {
	RelayLogRepairPreview
	BatchID string `json:"batch_id"`
	Updated int    `json:"updated"`
}

func RelayLogRepairPreviewRun(ctx context.Context, filter RelayLogRepairFilter) (RelayLogRepairPreview, error) {
	if err := RelayLogFlushPending(ctx); err != nil {
		return RelayLogRepairPreview{}, err
	}
	preview, err := scanRelayLogRepairCandidates(ctx, filter, nil)
	if err != nil {
		return RelayLogRepairPreview{}, err
	}
	auditID, err := newRepairAuditID("preview")
	if err != nil {
		return RelayLogRepairPreview{}, err
	}
	preview.AuditID = auditID
	if err := createRelayLogRepairAudit(ctx, auditID, preview, 0, true, time.Now()); err != nil {
		return RelayLogRepairPreview{}, err
	}
	return preview, nil
}

func RelayLogRepairExecute(ctx context.Context, filter RelayLogRepairFilter) (RelayLogRepairResult, error) {
	if err := RelayLogFlushPending(ctx); err != nil {
		return RelayLogRepairResult{}, err
	}
	batchID, err := newRepairAuditID("repair")
	if err != nil {
		return RelayLogRepairResult{}, err
	}

	matchedIDs := make([]int64, 0, 128)
	preview, err := scanRelayLogRepairCandidates(ctx, filter, &matchedIDs)
	if err != nil {
		return RelayLogRepairResult{}, err
	}

	updated := 0
	completedAt := time.Now()

	// Keep updates in small independent batches so SQLite never holds a large
	// write transaction for a GB-scale relay_logs table.
	for start := 0; start < len(matchedIDs); start += relayLogRepairBatchSize {
		end := start + relayLogRepairBatchSize
		if end > len(matchedIDs) {
			end = len(matchedIDs)
		}
		result := db.GetDB().WithContext(ctx).
			Model(&model.RelayLog{}).
			Where("id IN ? AND (repair_batch_id = '' OR repair_batch_id IS NULL)", matchedIDs[start:end]).
			Updates(map[string]interface{}{
				"original_outcome":      model.RequestOutcomeFailed,
				"original_error":        gorm.Expr("error"),
				"success":               true,
				"outcome":               model.RequestOutcomeSuccess,
				"error":                 "",
				"transport_termination": model.TransportTerminationClientDisconnectedAfterFinish,
				"completion_evidence":   model.CompletionEvidenceHistoricalRule,
				"repair_batch_id":       batchID,
				"repair_rule_version":   relayLogRepairRuleV1,
				"repaired_at":           completedAt,
			})
		if result.Error != nil {
			return RelayLogRepairResult{}, result.Error
		}
		updated += int(result.RowsAffected)
	}

	if err := createRelayLogRepairAudit(ctx, batchID, preview, updated, false, completedAt); err != nil {
		return RelayLogRepairResult{}, err
	}

	return RelayLogRepairResult{
		RelayLogRepairPreview: preview,
		BatchID:               batchID,
		Updated:               updated,
	}, nil
}

func scanRelayLogRepairCandidates(
	ctx context.Context,
	filter RelayLogRepairFilter,
	matchedIDs *[]int64,
) (RelayLogRepairPreview, error) {
	result := RelayLogRepairPreview{
		RuleVersion: relayLogRepairRuleV1,
		Reasons:     make(map[string]int),
		Samples:     make([]RelayLogRepairSample, 0, 20),
	}
	query := db.GetDB().WithContext(ctx).
		Model(&model.RelayLog{}).
		Where("success = ? AND LOWER(error) LIKE ?", false, "%context canceled%")
	if filter.StartTime != nil {
		query = query.Where("time >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("time <= ?", *filter.EndTime)
	}

	var rows []model.RelayLog
	if err := query.
		Select("id", "time", "request_model_name", "channel_name", "request_content", "response_content", "output_tokens", "outcome", "repair_batch_id").
		Order("id ASC").
		FindInBatches(&rows, relayLogRepairBatchSize, func(_ *gorm.DB, _ int) error {
			for i := range rows {
				ok, reason := relayLogRepairEligible(rows[i])
				if !ok {
					result.Excluded++
					result.Reasons[reason]++
					continue
				}
				result.Matched++
				if matchedIDs != nil {
					*matchedIDs = append(*matchedIDs, rows[i].ID)
				}
				if len(result.Samples) < 20 {
					result.Samples = append(result.Samples, RelayLogRepairSample{
						ID:           rows[i].ID,
						Time:         rows[i].Time,
						Model:        rows[i].RequestModelName,
						Channel:      rows[i].ChannelName,
						OutputTokens: rows[i].OutputTokens,
					})
				}
			}
			return nil
		}).Error; err != nil {
		return RelayLogRepairPreview{}, err
	}
	return result, nil
}

func relayLogRepairEligible(entry model.RelayLog) (bool, string) {
	if entry.RepairBatchID != "" {
		return false, "already_repaired"
	}
	if entry.Outcome != "" && entry.Outcome != model.RequestOutcomeFailed {
		return false, "not_failed"
	}
	if entry.OutputTokens <= 0 {
		return false, "no_output_tokens"
	}

	var request map[string]json.RawMessage
	if err := json.Unmarshal([]byte(entry.RequestContent), &request); err != nil {
		return false, "invalid_request_json"
	}
	var streamEnabled bool
	if raw, ok := request["stream"]; !ok || json.Unmarshal(raw, &streamEnabled) != nil || !streamEnabled {
		return false, "not_streaming"
	}
	if _, hasInput := request["input"]; !hasInput {
		if _, hasPrevious := request["previous_response_id"]; !hasPrevious {
			return false, "not_responses_request"
		}
	}

	var response transformerModel.InternalLLMResponse
	if err := json.Unmarshal([]byte(entry.ResponseContent), &response); err != nil {
		return false, "invalid_response_json"
	}
	if response.Error != nil {
		return false, "response_error"
	}
	for _, choice := range response.Choices {
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			return true, ""
		}
	}
	return false, "missing_finish_reason"
}

func createRelayLogRepairAudit(
	ctx context.Context,
	id string,
	preview RelayLogRepairPreview,
	updated int,
	dryRun bool,
	requestedAt time.Time,
) error {
	audit := model.RelayLogRepairAudit{
		BatchID:     id,
		RuleVersion: relayLogRepairRuleV1,
		DryRun:      dryRun,
		Matched:     preview.Matched,
		Updated:     updated,
		Excluded:    preview.Excluded,
		RequestedAt: requestedAt,
		CompletedAt: time.Now(),
	}
	return db.GetDB().WithContext(ctx).Create(&audit).Error
}

func newRepairAuditID(prefix string) (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate repair audit id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}
