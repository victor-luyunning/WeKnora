package fmsbridge

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuntimeServiceExposesMirrorOverviewAndRetrievalPreview(t *testing.T) {
	t.Setenv(envFMSBaseURL, "https://fms.example.test")
	t.Setenv(envFMSServiceToken, "service-token")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&mirrorRow{}, &artifactRow{}, &retrievalUnitRow{}, &syncRunRow{}); err != nil {
		t.Fatalf("migrate mirror tables: %v", err)
	}
	store := NewGormStore(db)
	snapshot := testSnapshot()
	snapshot.Book = BookMetadata{BookNo: "978-7-0000-0001-1", Title: "FMS 样书", Subject: "计算机"}
	if err := store.UpsertSnapshot(context.Background(), snapshot); err != nil {
		t.Fatalf("UpsertSnapshot: %v", err)
	}
	runID, err := store.BeginRun(context.Background())
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := store.FinishRun(context.Background(), runID, SyncResult{Seen: 1, Succeeded: 1}, nil); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	service := NewRuntimeService(db, &recordingTaskEnqueuer{})
	overview, err := service.Overview(context.Background(), 20)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !overview.Enabled || overview.MirrorCount != 1 || overview.LatestRun == nil {
		t.Fatalf("overview = %#v", overview)
	}
	if len(overview.Mirrors) != 1 || overview.Mirrors[0].Book.Title != "FMS 样书" || overview.Mirrors[0].RetrievalUnitCount != 1 {
		t.Fatalf("mirrors = %#v", overview.Mirrors)
	}

	detail, err := service.MirrorDetail(context.Background(), overview.Mirrors[0].ID, 20)
	if err != nil {
		t.Fatalf("MirrorDetail: %v", err)
	}
	if detail.Book.Subject != "计算机" || len(detail.RetrievalUnits) != 1 || detail.RetrievalUnits[0].ContentText != "首期镜像正文" {
		t.Fatalf("detail = %#v", detail)
	}
}

func testSnapshot() Snapshot {
	sidecars := map[string][]json.RawMessage{}
	for _, kind := range RequiredArtifactKinds {
		sidecars[kind] = []json.RawMessage{json.RawMessage(`{"kind":"` + kind + `"}`)}
	}
	sidecars[ArtifactKindRetrievalUnits] = []json.RawMessage{json.RawMessage(`{
        "unit_id":"unit-1","ordinal":1,"structural_node_id":"node-1",
        "content_type":"text","content_text":"首期镜像正文","locator":{"href":"chapter-1.xhtml"}
    }`)}
	return Snapshot{
		ArchiveRecordID: "record-1",
		AssetID:         "asset-1",
		SourceRef:       "fms:archive:record-1:asset:asset-1",
		Revision: SourceRevision{
			SourceChecksumSHA256: "checksum", RevisionKey: "revision-1",
		},
		ProjectionState: "ready",
		ReadinessStatus: "pass",
		HandoffEligible: true,
		Artifacts:       []Artifact{{Kind: ArtifactKindRetrievalUnits, Required: true, Available: true}},
		Sidecars:        sidecars,
	}
}
