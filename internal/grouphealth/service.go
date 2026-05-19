package grouphealth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

var ErrGroupHealthAlreadyRunning = errors.New("group health check already running")

type Repository interface {
	CreateRunningSnapshot(ctx context.Context, group model.Group, probeMode model.GroupHealthProbeMode) (*model.GroupHealthSnapshot, error)
	AppendAttempt(ctx context.Context, snapshotID int, attempt model.GroupHealthAttempt) error
	FinishSnapshot(ctx context.Context, snapshotID int, status model.GroupHealthStatus, successfulChannelID *int, durationMS int64, message string, finishedAt time.Time) error
	GetLatestSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error)
	GetRunningSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error)
	ListGroupHealthViews(ctx context.Context) ([]model.GroupHealthGroupView, error)
	GetGroupHealthViewByID(ctx context.Context, groupID int) (*model.GroupHealthGroupView, error)
}

type Service struct {
	repo   Repository
	prober *Prober
}

var runLocks sync.Map

func NewService(repo Repository, prober *Prober) *Service {
	if repo == nil {
		repo = op.NewGroupHealthRepository()
	}
	if prober == nil {
		prober = NewProber()
	}
	return &Service{
		repo:   repo,
		prober: prober,
	}
}

func lockGroup(groupID int) func() {
	value, _ := runLocks.LoadOrStore(groupID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return func() {
		lock.Unlock()
	}
}

// normalizeProbeMode returns the effective probe mode from a prioritized list.
// An empty list defaults to model.GroupHealthProbeModeStandard, and only the
// first element is considered. model.GroupHealthProbeModeFull is honored only
// when it appears first; all other cases fall back to Standard semantics.
func normalizeProbeMode(probeModes []model.GroupHealthProbeMode) model.GroupHealthProbeMode {
	if len(probeModes) == 0 {
		return model.GroupHealthProbeModeStandard
	}
	if probeModes[0] == model.GroupHealthProbeModeFull {
		return model.GroupHealthProbeModeFull
	}
	return model.GroupHealthProbeModeStandard
}

func resolveChannelName(ctx context.Context, channelID int) string {
	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return fmt.Sprintf("channel-%d", channelID)
	}
	return channel.Name
}

func (s *Service) RunGroupHealth(ctx context.Context, groupID int, probeModes ...model.GroupHealthProbeMode) error {
	unlock := lockGroup(groupID)
	defer unlock()

	if _, err := s.repo.GetRunningSnapshotByGroupID(ctx, groupID); err == nil {
		return ErrGroupHealthAlreadyRunning
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	group, err := op.GroupGet(groupID, ctx)
	if err != nil {
		return err
	}

	probeMode := normalizeProbeMode(probeModes)

	snapshot, err := s.repo.CreateRunningSnapshot(ctx, *group, probeMode)
	if err != nil {
		return err
	}

	items := append([]model.GroupItem(nil), group.Items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if items[i].Weight != items[j].Weight {
			return items[i].Weight > items[j].Weight
		}
		if items[i].ChannelID != items[j].ChannelID {
			return items[i].ChannelID < items[j].ChannelID
		}
		return items[i].ID < items[j].ID
	})

	var successfulChannelID *int
	message := "all candidates failed"
	stopAfterSuccess := group.Mode == model.GroupModeFailover && probeMode != model.GroupHealthProbeModeFull
	successFound := false
	firstSuccessIndex := -1
	attemptedCount := 0
	successCount := 0

	for index, item := range items {
		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			attemptedCount++
			appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
				GroupItemID:  item.ID,
				ChannelID:    item.ChannelID,
				ChannelName:  fmt.Sprintf("channel-%d", item.ChannelID),
				ModelName:    item.ModelName,
				Priority:     item.Priority,
				Weight:       item.Weight,
				Status:       model.GroupHealthAttemptStatusFailed,
				ErrorMessage: fmt.Sprintf("failed to load channel: %v", err),
			})
			if appendErr != nil {
				return appendErr
			}
			continue
		}

		usedKey := channel.GetChannelKey()
		if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" {
			attemptedCount++
			appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
				GroupItemID:  item.ID,
				ChannelID:    item.ChannelID,
				ChannelName:  channel.Name,
				ModelName:    item.ModelName,
				Priority:     item.Priority,
				Weight:       item.Weight,
				Status:       model.GroupHealthAttemptStatusFailed,
				ErrorMessage: "no available key",
			})
			if appendErr != nil {
				return appendErr
			}
			continue
		}

		result := s.prober.RunCandidate(ctx, *channel, usedKey, item.ModelName)
		attemptedCount++
		attempt := model.GroupHealthAttempt{
			GroupItemID:  item.ID,
			ChannelID:    item.ChannelID,
			ChannelName:  channel.Name,
			ChannelKeyID: usedKey.ID,
			KeyRemark:    usedKey.Remark,
			ModelName:    item.ModelName,
			Priority:     item.Priority,
			Weight:       item.Weight,
			HTTPStatus:   result.HTTPStatus,
			DurationMS:   result.DurationMS,
			ErrorMessage: result.ErrorMessage,
		}
		if result.Success {
			attempt.Status = model.GroupHealthAttemptStatusSuccess
		} else {
			attempt.Status = model.GroupHealthAttemptStatusFailed
		}
		if err := s.repo.AppendAttempt(ctx, snapshot.ID, attempt); err != nil {
			return err
		}

		if result.Success {
			successFound = true
			successCount++
			if firstSuccessIndex == -1 {
				firstSuccessIndex = index
				successfulChannelID = &item.ChannelID
			}
			if stopAfterSuccess {
				for _, skipped := range items[index+1:] {
					channelName := fmt.Sprintf("channel-%d", skipped.ChannelID)
					if skippedChannel, getErr := op.ChannelGet(skipped.ChannelID, ctx); getErr == nil {
						channelName = skippedChannel.Name
					}
					if err := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
						GroupItemID: skipped.ID,
						ChannelID:   skipped.ChannelID,
						ChannelName: channelName,
						ModelName:   skipped.ModelName,
						Priority:    skipped.Priority,
						Weight:      skipped.Weight,
						Status:      model.GroupHealthAttemptStatusSkipped,
					}); err != nil {
						return err
					}
				}
				break
			}
		}
	}

	finalStatus := model.GroupHealthStatusFailed
	if !successFound && len(items) == 0 {
		message = "group has no items"
	} else if successFound {
		successChannelName := resolveChannelName(ctx, items[firstSuccessIndex].ChannelID)
		switch {
		case stopAfterSuccess && firstSuccessIndex == 0:
			finalStatus = model.GroupHealthStatusSuccess
			message = fmt.Sprintf("candidate %s succeeded", successChannelName)
		case stopAfterSuccess:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("candidate %s succeeded after failover", successChannelName)
		case successCount == attemptedCount:
			finalStatus = model.GroupHealthStatusSuccess
			message = fmt.Sprintf("all %d candidates succeeded", successCount)
		default:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("%d/%d candidates succeeded", successCount, attemptedCount)
		}
	}

	finishedAt := time.Now()
	durationMS := finishedAt.Sub(snapshot.StartedAt).Milliseconds()
	return s.repo.FinishSnapshot(ctx, snapshot.ID, finalStatus, successfulChannelID, durationMS, message, finishedAt)
}

func (s *Service) RunAllGroupHealth(ctx context.Context, maxConcurrency int, probeModes ...model.GroupHealthProbeMode) {
	if maxConcurrency <= 0 {
		maxConcurrency = 2
	}
	probeMode := normalizeProbeMode(probeModes)
	groups, err := op.GroupList(ctx)
	if err != nil {
		return
	}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for _, group := range groups {
		groupID := group.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			_ = s.RunGroupHealth(ctx, groupID, probeMode)
		}()
	}
	wg.Wait()
}

func (s *Service) ListGroupHealthViews(ctx context.Context) ([]model.GroupHealthGroupView, error) {
	return s.repo.ListGroupHealthViews(ctx)
}

func (s *Service) GetGroupHealthViewByID(ctx context.Context, groupID int) (*model.GroupHealthGroupView, error) {
	return s.repo.GetGroupHealthViewByID(ctx, groupID)
}

func (s *Service) GetRunningSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error) {
	return s.repo.GetRunningSnapshotByGroupID(ctx, groupID)
}
