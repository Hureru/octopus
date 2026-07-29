package op

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

const routeCandidateHealthWindow = 24 * time.Hour

type routeCandidatePerformance struct {
	RouteCandidateID  int   `gorm:"column:route_candidate_id"`
	SuccessCount      int64 `gorm:"column:success_count"`
	FailureCount      int64 `gorm:"column:failure_count"`
	SuccessDurationMS int64 `gorm:"column:success_duration_ms"`
}

func routeCandidatePerformanceMap(
	ctx context.Context,
	candidateIDs []int,
	now time.Time,
) (map[int]routeCandidatePerformance, error) {
	result := make(map[int]routeCandidatePerformance, len(candidateIDs))
	if len(candidateIDs) == 0 {
		return result, nil
	}
	var rows []routeCandidatePerformance
	err := db.GetDB().WithContext(ctx).
		Model(&model.UsageAttemptFact{}).
		Select(
			"route_candidate_id, "+
				"SUM(CASE WHEN status = ? THEN 1 ELSE 0 END) AS success_count, "+
				"SUM(CASE WHEN status = ? AND attribution = ? THEN 1 ELSE 0 END) AS failure_count, "+
				"SUM(CASE WHEN status = ? THEN duration_ms ELSE 0 END) AS success_duration_ms",
			model.AttemptSuccess,
			model.AttemptFailed,
			model.AttemptAttributionUpstream,
			model.AttemptSuccess,
		).
		Where("route_candidate_id IN ? AND time >= ?", candidateIDs, now.Add(-routeCandidateHealthWindow).Unix()).
		Group("route_candidate_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.RouteCandidateID] = row
	}
	return result, nil
}

func RouteCandidateHealthRefresh(
	ctx context.Context,
	now time.Time,
	minSamples int64,
) (int64, error) {
	if minSamples <= 0 {
		minSamples = 5
	}
	var candidates []model.RouteCandidate
	if err := db.GetDB().WithContext(ctx).
		Where("manual = ? AND status IN ?", false, []model.RouteCandidateStatus{
			model.RouteCandidateActive,
			model.RouteCandidateDegraded,
		}).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	performance, err := routeCandidatePerformanceMap(ctx, ids, now)
	if err != nil {
		return 0, err
	}

	var updated int64
	for _, candidate := range candidates {
		stats := performance[candidate.ID]
		total := stats.SuccessCount + stats.FailureCount
		if total < minSamples {
			continue
		}
		status := model.RouteCandidateActive
		if stats.FailureCount*2 >= total {
			status = model.RouteCandidateDegraded
		}
		if status == candidate.Status {
			continue
		}
		result := db.GetDB().WithContext(ctx).
			Model(&model.RouteCandidate{}).
			Where("id = ? AND manual = ? AND status IN ?", candidate.ID, false, []model.RouteCandidateStatus{
				model.RouteCandidateActive,
				model.RouteCandidateDegraded,
			}).
			Update("status", status)
		if result.Error != nil {
			return updated, result.Error
		}
		updated += result.RowsAffected
	}
	return updated, nil
}

func CatalogRouteCandidatesMarkStaleByAccount(ctx context.Context, accountID int) error {
	if accountID <= 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Model(&model.RouteCandidate{}).
		Where(
			"site_account_id = ? AND manual = ? AND status IN ?",
			accountID,
			false,
			[]model.RouteCandidateStatus{
				model.RouteCandidateActive,
				model.RouteCandidateDegraded,
			},
		).
		Update("status", model.RouteCandidateStale).Error
}
