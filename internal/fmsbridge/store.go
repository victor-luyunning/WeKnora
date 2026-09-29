package fmsbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	sourceSystemFMS  = "fms"
	runStatusRunning = "running"
	runStatusSuccess = "success"
	runStatusPartial = "partial"
	runStatusFailed  = "failed"
)

// GormStore is WeKnora's rebuildable projection mirror. It stores only data
// fetched from FMS's controlled API, never archive files or storage paths.
// A source revision is immutable: a newer revision creates another mirror row
// and never overwrites a last-known-good revision.
type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) UpsertSnapshot(ctx context.Context, snapshot Snapshot) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("FMS mirror store is not configured")
	}
	if err := snapshot.validate(); err != nil {
		return err
	}
	artifacts, units, err := snapshotRows(snapshot)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing mirrorRow
		err := tx.Where("source_system = ? AND source_ref = ? AND revision_key = ?", sourceSystemFMS, snapshot.SourceRef, snapshot.Revision.RevisionKey).
			First(&existing).Error
		if err == nil {
			return tx.Model(&mirrorRow{}).Where("id = ?", existing.ID).Updates(map[string]any{
				"last_synced_at": now,
				"updated_at":     now,
			}).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		mirror := mirrorRow{
			ID:                   uuid.NewString(),
			SourceSystem:         sourceSystemFMS,
			SourceRef:            snapshot.SourceRef,
			RevisionKey:          snapshot.Revision.RevisionKey,
			ArchiveRecordID:      snapshot.ArchiveRecordID,
			AssetID:              snapshot.AssetID,
			BookNo:               snapshot.Book.BookNo,
			BookTitle:            snapshot.Book.Title,
			BookSubject:          snapshot.Book.Subject,
			SourceChecksumSHA256: snapshot.Revision.SourceChecksumSHA256,
			ParserVersion:        snapshot.Revision.ParserVersion,
			SegmentPolicyVersion: snapshot.Revision.SegmentPolicyVersion,
			EmbeddingPolicy:      snapshot.Revision.EmbeddingPolicy,
			ProjectionState:      snapshot.ProjectionState,
			ReadinessStatus:      snapshot.ReadinessStatus,
			HandoffEligible:      snapshot.HandoffEligible,
			Artifacts:            mustJSON(snapshot.Artifacts),
			CreatedAt:            now,
			UpdatedAt:            now,
			LastSyncedAt:         now,
		}
		if err := tx.Create(&mirror).Error; err != nil {
			return err
		}
		for index := range artifacts {
			artifacts[index].ID = uuid.NewString()
			artifacts[index].MirrorID = mirror.ID
		}
		for index := range units {
			units[index].ID = uuid.NewString()
			units[index].MirrorID = mirror.ID
		}
		if len(artifacts) > 0 {
			if err := tx.CreateInBatches(artifacts, 500).Error; err != nil {
				return err
			}
		}
		if len(units) > 0 {
			if err := tx.CreateInBatches(units, 500).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// BeginRun and FinishRun let Reconciler expose explainable per-run outcomes
// without turning a partial list traversal into an accidental source deletion.
func (s *GormStore) BeginRun(ctx context.Context) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("FMS mirror store is not configured")
	}
	run := syncRunRow{
		ID: uuid.NewString(), Status: runStatusRunning, StartedAt: time.Now().UTC(),
		Summary: mustJSON(map[string]any{}), ErrorMessage: "",
	}
	if err := s.db.WithContext(ctx).Create(&run).Error; err != nil {
		return "", err
	}
	return run.ID, nil
}

func (s *GormStore) FinishRun(ctx context.Context, runID string, result SyncResult, runErr error) error {
	if runID == "" {
		return nil
	}
	status := runStatusSuccess
	if runErr != nil {
		status = runStatusFailed
	} else if result.Failed > 0 {
		status = runStatusPartial
	}
	summary := mustJSON(map[string]any{
		"seen": result.Seen, "succeeded": result.Succeeded, "skipped": result.Skipped,
		"failed": result.Failed, "failures": result.Failures,
	})
	updates := map[string]any{"status": status, "finished_at": time.Now().UTC(), "summary": summary, "error_message": ""}
	if runErr != nil {
		updates["error_message"] = runErr.Error()
	}
	return s.db.WithContext(ctx).Model(&syncRunRow{}).Where("id = ?", runID).Updates(updates).Error
}

type runStore interface {
	MirrorStore
	BeginRun(ctx context.Context) (string, error)
	FinishRun(ctx context.Context, runID string, result SyncResult, runErr error) error
}

type mirrorRow struct {
	ID                   string     `gorm:"type:varchar(36);primaryKey"`
	SourceSystem         string     `gorm:"type:varchar(32);not null"`
	SourceRef            string     `gorm:"type:text;not null"`
	RevisionKey          string     `gorm:"type:varchar(64);not null"`
	ArchiveRecordID      string     `gorm:"type:varchar(36);not null"`
	AssetID              string     `gorm:"type:varchar(36);not null"`
	BookNo               string     `gorm:"type:text;not null"`
	BookTitle            string     `gorm:"type:text;not null"`
	BookSubject          string     `gorm:"type:text;not null"`
	SourceChecksumSHA256 string     `gorm:"type:varchar(64);not null"`
	ParserVersion        string     `gorm:"type:text;not null"`
	SegmentPolicyVersion string     `gorm:"type:text;not null"`
	EmbeddingPolicy      string     `gorm:"type:text;not null"`
	ProjectionState      string     `gorm:"type:varchar(32);not null"`
	ReadinessStatus      string     `gorm:"type:varchar(32);not null"`
	HandoffEligible      bool       `gorm:"not null"`
	Artifacts            types.JSON `gorm:"type:jsonb;not null"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastSyncedAt         time.Time `gorm:"not null"`
}

func (mirrorRow) TableName() string { return "fms_projection_mirrors" }

type artifactRow struct {
	ID          string     `gorm:"type:varchar(36);primaryKey"`
	MirrorID    string     `gorm:"type:varchar(36);not null"`
	Kind        string     `gorm:"type:varchar(64);not null"`
	RecordIndex int        `gorm:"not null"`
	Payload     types.JSON `gorm:"type:jsonb;not null"`
}

func (artifactRow) TableName() string { return "fms_mirror_artifact_records" }

type retrievalUnitRow struct {
	ID               string     `gorm:"type:varchar(36);primaryKey"`
	MirrorID         string     `gorm:"type:varchar(36);not null"`
	UnitID           string     `gorm:"type:text;not null"`
	Ordinal          int        `gorm:"not null"`
	StructuralNodeID string     `gorm:"type:text;not null"`
	ContentType      string     `gorm:"type:varchar(64);not null"`
	ContentText      string     `gorm:"type:text;not null"`
	Locator          types.JSON `gorm:"type:jsonb;not null"`
	Payload          types.JSON `gorm:"type:jsonb;not null"`
}

func (retrievalUnitRow) TableName() string { return "fms_mirror_retrieval_units" }

type syncRunRow struct {
	ID           string    `gorm:"type:varchar(36);primaryKey"`
	Status       string    `gorm:"type:varchar(32);not null"`
	StartedAt    time.Time `gorm:"not null"`
	FinishedAt   *time.Time
	Summary      types.JSON `gorm:"type:jsonb;not null"`
	ErrorMessage string     `gorm:"type:text;not null"`
}

func (syncRunRow) TableName() string { return "fms_sync_runs" }

func (s *GormStore) ListMirrors(ctx context.Context, limit int) ([]MirrorSummary, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("FMS mirror store is not configured")
	}
	if limit < 1 || limit > 100 {
		return nil, 0, fmt.Errorf("FMS mirror limit must be between 1 and 100")
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&mirrorRow{}).Where("source_system = ?", sourceSystemFMS).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []mirrorRow
	if err := s.db.WithContext(ctx).Where("source_system = ?", sourceSystemFMS).
		Order("last_synced_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	result := make([]MirrorSummary, 0, len(rows))
	for _, row := range rows {
		var unitCount int64
		if err := s.db.WithContext(ctx).Model(&retrievalUnitRow{}).Where("mirror_id = ?", row.ID).Count(&unitCount).Error; err != nil {
			return nil, 0, err
		}
		result = append(result, mirrorSummaryFromRow(row, unitCount))
	}
	return result, total, nil
}

func (s *GormStore) MirrorDetail(ctx context.Context, id string, limit int) (MirrorDetail, error) {
	if s == nil || s.db == nil {
		return MirrorDetail{}, fmt.Errorf("FMS mirror store is not configured")
	}
	if id == "" {
		return MirrorDetail{}, fmt.Errorf("FMS mirror id is required")
	}
	if limit < 1 || limit > 100 {
		return MirrorDetail{}, fmt.Errorf("FMS retrieval preview limit must be between 1 and 100")
	}
	var row mirrorRow
	if err := s.db.WithContext(ctx).Where("id = ? AND source_system = ?", id, sourceSystemFMS).First(&row).Error; err != nil {
		return MirrorDetail{}, err
	}
	var unitCount int64
	if err := s.db.WithContext(ctx).Model(&retrievalUnitRow{}).Where("mirror_id = ?", row.ID).Count(&unitCount).Error; err != nil {
		return MirrorDetail{}, err
	}
	var artifacts []Artifact
	if err := json.Unmarshal(row.Artifacts, &artifacts); err != nil {
		return MirrorDetail{}, fmt.Errorf("decode FMS mirror artifacts: %w", err)
	}
	var rows []retrievalUnitRow
	if err := s.db.WithContext(ctx).Where("mirror_id = ?", row.ID).Order("ordinal ASC").Limit(limit).Find(&rows).Error; err != nil {
		return MirrorDetail{}, err
	}
	units := make([]RetrievalUnitPreview, 0, len(rows))
	for _, unit := range rows {
		units = append(units, RetrievalUnitPreview{
			UnitID: unit.UnitID, Ordinal: unit.Ordinal, StructuralNodeID: unit.StructuralNodeID,
			ContentType: unit.ContentType, ContentText: unit.ContentText, Locator: json.RawMessage(unit.Locator),
		})
	}
	return MirrorDetail{MirrorSummary: mirrorSummaryFromRow(row, unitCount), Artifacts: artifacts, RetrievalUnits: units}, nil
}

func (s *GormStore) MirrorRef(ctx context.Context, id string) (ProjectionRef, error) {
	if s == nil || s.db == nil {
		return ProjectionRef{}, fmt.Errorf("FMS mirror store is not configured")
	}
	if id == "" {
		return ProjectionRef{}, fmt.Errorf("FMS mirror id is required")
	}
	var row mirrorRow
	if err := s.db.WithContext(ctx).Where("id = ? AND source_system = ?", id, sourceSystemFMS).First(&row).Error; err != nil {
		return ProjectionRef{}, err
	}
	return ProjectionRef{ArchiveRecordID: row.ArchiveRecordID, AssetID: row.AssetID}, nil
}

func (s *GormStore) LatestRun(ctx context.Context) (*SyncRunSummary, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("FMS mirror store is not configured")
	}
	var row syncRunRow
	err := s.db.WithContext(ctx).Order("started_at DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	metrics := SyncRunMetrics{}
	if err := json.Unmarshal(row.Summary, &metrics); err != nil {
		return nil, fmt.Errorf("decode FMS sync summary: %w", err)
	}
	return &SyncRunSummary{ID: row.ID, Status: row.Status, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, Summary: metrics, ErrorMessage: row.ErrorMessage}, nil
}

func mirrorSummaryFromRow(row mirrorRow, unitCount int64) MirrorSummary {
	return MirrorSummary{
		ID: row.ID, SourceRef: row.SourceRef, RevisionKey: row.RevisionKey,
		ArchiveRecordID: row.ArchiveRecordID, AssetID: row.AssetID,
		Book:            BookMetadata{BookNo: row.BookNo, Title: row.BookTitle, Subject: row.BookSubject},
		ProjectionState: row.ProjectionState, ReadinessStatus: row.ReadinessStatus,
		HandoffEligible: row.HandoffEligible, RetrievalUnitCount: unitCount, LastSyncedAt: row.LastSyncedAt,
	}
}

func snapshotRows(snapshot Snapshot) ([]artifactRow, []retrievalUnitRow, error) {
	artifacts := make([]artifactRow, 0)
	units := make([]retrievalUnitRow, 0)
	for _, kind := range RequiredArtifactKinds {
		for index, record := range snapshot.Sidecars[kind] {
			if kind == ArtifactKindRetrievalUnits {
				unit, err := decodeRetrievalUnit(record)
				if err != nil {
					return nil, nil, fmt.Errorf("decode retrieval unit %d: %w", index, err)
				}
				units = append(units, unit)
				continue
			}
			artifacts = append(artifacts, artifactRow{Kind: kind, RecordIndex: index, Payload: types.JSON(record)})
		}
	}
	if len(units) == 0 {
		return nil, nil, fmt.Errorf("eligible FMS projection has no retrieval units")
	}
	return artifacts, units, nil
}

func decodeRetrievalUnit(record json.RawMessage) (retrievalUnitRow, error) {
	var payload struct {
		UnitID           string          `json:"unit_id"`
		Ordinal          int             `json:"ordinal"`
		StructuralNodeID string          `json:"structural_node_id"`
		ContentType      string          `json:"content_type"`
		ContentText      string          `json:"content_text"`
		Locator          json.RawMessage `json:"locator"`
	}
	if err := json.Unmarshal(record, &payload); err != nil {
		return retrievalUnitRow{}, err
	}
	if payload.UnitID == "" || payload.StructuralNodeID == "" || payload.ContentText == "" || len(payload.Locator) == 0 {
		return retrievalUnitRow{}, fmt.Errorf("unit_id, structural_node_id, content_text, and locator are required")
	}
	if payload.ContentType == "" {
		payload.ContentType = "text"
	}
	return retrievalUnitRow{
		UnitID: payload.UnitID, Ordinal: payload.Ordinal, StructuralNodeID: payload.StructuralNodeID,
		ContentType: payload.ContentType, ContentText: payload.ContentText,
		Locator: types.JSON(payload.Locator), Payload: types.JSON(record),
	}, nil
}

func mustJSON(value any) types.JSON {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("FMS bridge JSON encoding failed: %v", err))
	}
	return types.JSON(encoded)
}
