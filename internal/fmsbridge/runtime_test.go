package fmsbridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuntimeServiceEnqueuesExplicitReconcileOnlyWhenEnabled(t *testing.T) {
	t.Setenv(envFMSBaseURL, "https://fms.example.test")
	t.Setenv(envFMSServiceToken, "service-token")
	enqueuer := &recordingTaskEnqueuer{}
	service := NewRuntimeService(nil, enqueuer)

	info, err := service.EnqueueReconcile(context.Background())
	if err != nil {
		t.Fatalf("EnqueueReconcile() error = %v", err)
	}
	if info == nil || enqueuer.task == nil || enqueuer.task.Type() != types.TypeFMSBridgeReconcile {
		t.Fatalf("enqueued task = %#v info = %#v", enqueuer.task, info)
	}
}

func TestRuntimeServiceRejectsReconcileWhenBridgeIsDisabled(t *testing.T) {
	t.Setenv(envFMSBaseURL, "")
	t.Setenv(envFMSServiceToken, "")
	service := NewRuntimeService(nil, &recordingTaskEnqueuer{})

	_, err := service.EnqueueReconcile(context.Background())
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("EnqueueReconcile() error = %v, want ErrDisabled", err)
	}
}

func TestRuntimeServiceEnqueuesSingleMirrorReconcile(t *testing.T) {
	t.Setenv(envFMSBaseURL, "https://fms.example.test")
	t.Setenv(envFMSServiceToken, "service-token")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&mirrorRow{}); err != nil {
		t.Fatalf("migrate mirror table: %v", err)
	}
	if err := db.Create(&mirrorRow{
		ID: "mirror-1", SourceSystem: sourceSystemFMS,
		SourceRef: "fms:archive:record-1:asset:asset-1", RevisionKey: "revision-1",
		ArchiveRecordID: "record-1", AssetID: "asset-1", BookNo: "book-1",
		BookTitle: "样书", BookSubject: "计算机", SourceChecksumSHA256: "checksum",
		ParserVersion: "parser", SegmentPolicyVersion: "segment", EmbeddingPolicy: "embedding",
		ProjectionState: "ready", ReadinessStatus: "review", HandoffEligible: true,
		Artifacts: types.JSON(`[]`), LastSyncedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create mirror: %v", err)
	}

	enqueuer := &recordingTaskEnqueuer{}
	service := NewRuntimeService(db, enqueuer)
	info, err := service.EnqueueMirrorReconcile(context.Background(), "mirror-1")
	if err != nil {
		t.Fatalf("EnqueueMirrorReconcile() error = %v", err)
	}
	if info == nil || enqueuer.task == nil || enqueuer.task.Type() != types.TypeFMSBridgeReconcile {
		t.Fatalf("enqueued task = %#v info = %#v", enqueuer.task, info)
	}
	var payload ReconcilePayload
	if err := json.Unmarshal(enqueuer.task.Payload(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.ArchiveRecordID != "record-1" || payload.AssetID != "asset-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRuntimeServiceRejectsUnknownMirrorReconcile(t *testing.T) {
	t.Setenv(envFMSBaseURL, "https://fms.example.test")
	t.Setenv(envFMSServiceToken, "service-token")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&mirrorRow{}); err != nil {
		t.Fatalf("migrate mirror table: %v", err)
	}

	_, err = NewRuntimeService(db, &recordingTaskEnqueuer{}).EnqueueMirrorReconcile(context.Background(), "missing")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("error = %v, want gorm.ErrRecordNotFound", err)
	}
}

type recordingTaskEnqueuer struct {
	task *asynq.Task
}

func (e *recordingTaskEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	e.task = task
	return &asynq.TaskInfo{ID: "queued-fms-bridge-run", Type: task.Type()}, nil
}
