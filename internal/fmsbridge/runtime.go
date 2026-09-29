package fmsbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
)

// ErrDisabled means this deployment has not opted into the FMS source profile.
// It is distinct from a failed FMS request so callers can return an actionable
// operator response without exposing source configuration or credentials.
var ErrDisabled = errors.New("FMS bridge is disabled")

// RuntimeService owns explicit bridge runs. It never schedules itself: an
// operator must trigger a reconciliation, which keeps the initial integration
// deliberately one-way and observable.
type RuntimeService struct {
	db       *gorm.DB
	enqueuer interfaces.TaskEnqueuer
}

// ReconcilePayload optionally narrows the regular reconciliation task to one
// FMS projection. An empty payload remains the full-scan contract used by the
// existing administrator action and older queued tasks.
type ReconcilePayload struct {
	ArchiveRecordID string `json:"archive_record_id,omitempty"`
	AssetID         string `json:"asset_id,omitempty"`
}

func NewRuntimeService(db *gorm.DB, enqueuer interfaces.TaskEnqueuer) *RuntimeService {
	return &RuntimeService{db: db, enqueuer: enqueuer}
}

func (s *RuntimeService) EnqueueReconcile(_ context.Context) (*asynq.TaskInfo, error) {
	return s.enqueueReconcile(ReconcilePayload{})
}

func (s *RuntimeService) EnqueueMirrorReconcile(ctx context.Context, mirrorID string) (*asynq.TaskInfo, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("FMS bridge database is not configured")
	}
	ref, err := NewGormStore(s.db).MirrorRef(ctx, mirrorID)
	if err != nil {
		return nil, err
	}
	return s.enqueueReconcile(ReconcilePayload{
		ArchiveRecordID: ref.ArchiveRecordID,
		AssetID:         ref.AssetID,
	})
}

func (s *RuntimeService) enqueueReconcile(payloadData ReconcilePayload) (*asynq.TaskInfo, error) {
	if s == nil || s.enqueuer == nil {
		return nil, fmt.Errorf("FMS bridge task enqueuer is not configured")
	}
	if _, enabled, err := ConfigFromEnvironment(); err != nil {
		return nil, err
	} else if !enabled {
		return nil, ErrDisabled
	}
	payload, err := json.Marshal(payloadData)
	if err != nil {
		return nil, fmt.Errorf("encode FMS bridge task: %w", err)
	}
	task := asynq.NewTask(types.TypeFMSBridgeReconcile, payload,
		asynq.Queue(types.QueueSync), asynq.MaxRetry(3), asynq.Timeout(time.Hour))
	info, err := s.enqueuer.Enqueue(task)
	if err != nil {
		return nil, fmt.Errorf("enqueue FMS bridge reconciliation: %w", err)
	}
	return info, nil
}

// Overview is a read-only administrative projection of the local FMS mirror.
func (s *RuntimeService) Overview(ctx context.Context, limit int) (BridgeOverview, error) {
	if s == nil || s.db == nil {
		return BridgeOverview{}, fmt.Errorf("FMS bridge database is not configured")
	}
	overview := BridgeOverview{Mirrors: []MirrorSummary{}}
	if _, enabled, err := ConfigFromEnvironment(); err != nil {
		overview.ConfigurationIssue = "FMS source configuration is incomplete"
	} else {
		overview.Enabled = enabled
	}
	store := NewGormStore(s.db)
	mirrors, count, err := store.ListMirrors(ctx, limit)
	if err != nil {
		return BridgeOverview{}, err
	}
	latestRun, err := store.LatestRun(ctx)
	if err != nil {
		return BridgeOverview{}, err
	}
	overview.MirrorCount, overview.Mirrors, overview.LatestRun = count, mirrors, latestRun
	return overview, nil
}

func (s *RuntimeService) MirrorDetail(ctx context.Context, id string, limit int) (MirrorDetail, error) {
	if s == nil || s.db == nil {
		return MirrorDetail{}, fmt.Errorf("FMS bridge database is not configured")
	}
	return NewGormStore(s.db).MirrorDetail(ctx, id, limit)
}

// ProcessReconcile is the worker entry point shared by Redis and Lite mode.
// The task carries no source selection: the only source is the deployment's
// dedicated FMS profile, and its token is read fresh from the environment.
func (s *RuntimeService) ProcessReconcile(ctx context.Context, task *asynq.Task) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("FMS bridge database is not configured")
	}
	config, enabled, err := ConfigFromEnvironment()
	if err != nil {
		return err
	}
	if !enabled {
		return ErrDisabled
	}
	adapter, err := NewAdapter(config)
	if err != nil {
		return err
	}
	var payload ReconcilePayload
	if task != nil && len(task.Payload()) > 0 {
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return fmt.Errorf("decode FMS bridge reconciliation payload: %w", err)
		}
	}
	reconciler := NewReconciler(adapter, NewGormStore(s.db))
	var result SyncResult
	if payload.ArchiveRecordID != "" || payload.AssetID != "" {
		if payload.ArchiveRecordID == "" || payload.AssetID == "" {
			return fmt.Errorf("FMS bridge reconciliation payload must include archive_record_id and asset_id")
		}
		result, err = reconciler.ReconcileOne(ctx, ProjectionRef{
			ArchiveRecordID: payload.ArchiveRecordID,
			AssetID:         payload.AssetID,
		})
	} else {
		result, err = reconciler.Reconcile(ctx)
	}
	if err != nil {
		return err
	}
	logger.Infof(ctx, "FMS bridge reconciliation finished: seen=%d succeeded=%d skipped=%d failed=%d",
		result.Seen, result.Succeeded, result.Skipped, result.Failed)
	return nil
}
