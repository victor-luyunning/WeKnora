package fmsbridge

import (
	"context"
	"encoding/json"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGormStoreKeepsOnlyLatestRevisionPerFMSProjection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:fmsbridge-store-latest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&mirrorRow{}, &artifactRow{}, &retrievalUnitRow{}, &syncRunRow{}); err != nil {
		t.Fatalf("migrate mirror tables: %v", err)
	}

	store := NewGormStore(db)
	ctx := context.Background()
	if err := store.UpsertSnapshot(ctx, latestOnlyTestSnapshot("record-1", "asset-1", "revision-1", "old")); err != nil {
		t.Fatalf("upsert first revision: %v", err)
	}
	if err := store.UpsertSnapshot(ctx, latestOnlyTestSnapshot("record-1", "asset-1", "revision-2", "new")); err != nil {
		t.Fatalf("upsert latest revision: %v", err)
	}

	mirrors, total, err := store.ListMirrors(ctx, 1, 100, MirrorListFilter{})
	if err != nil {
		t.Fatalf("list mirrors: %v", err)
	}
	if total != 1 || len(mirrors) != 1 {
		t.Fatalf("mirror count = total %d, rows %d; want one latest mirror", total, len(mirrors))
	}
	if mirrors[0].RevisionKey != "revision-2" {
		t.Fatalf("revision = %q, want latest revision", mirrors[0].RevisionKey)
	}
	if mirrors[0].Book.StandardClassification == nil || mirrors[0].Book.StandardClassification.PrimaryCode != "F" {
		t.Fatalf("classification = %#v", mirrors[0].Book.StandardClassification)
	}

	var artifactCount int64
	if err := db.Model(&artifactRow{}).Count(&artifactCount).Error; err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifactCount != int64(len(RequiredArtifactKinds)-1) {
		t.Fatalf("artifact count = %d, want latest revision artifacts only", artifactCount)
	}
	var unit retrievalUnitRow
	if err := db.First(&unit).Error; err != nil {
		t.Fatalf("load retrieval unit: %v", err)
	}
	if unit.UnitID != "unit-new" {
		t.Fatalf("retrieval unit = %q, want latest revision unit", unit.UnitID)
	}
}

func TestGormStoreKeepsDifferentFMSProjectionAssetsSeparate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:fmsbridge-store-assets?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&mirrorRow{}, &artifactRow{}, &retrievalUnitRow{}, &syncRunRow{}); err != nil {
		t.Fatalf("migrate mirror tables: %v", err)
	}

	store := NewGormStore(db)
	ctx := context.Background()
	for _, snapshot := range []Snapshot{
		latestOnlyTestSnapshot("record-1", "asset-1", "revision-1", "one"),
		latestOnlyTestSnapshot("record-1", "asset-2", "revision-1", "two"),
	} {
		if err := store.UpsertSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("upsert projection: %v", err)
		}
	}

	_, total, err := store.ListMirrors(ctx, 1, 100, MirrorListFilter{})
	if err != nil {
		t.Fatalf("list mirrors: %v", err)
	}
	if total != 2 {
		t.Fatalf("mirror count = %d, want two assets from one archive record", total)
	}
}

func TestGormStoreListsMirrorsByPageAndFilter(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:fmsbridge-store-list?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&mirrorRow{}, &artifactRow{}, &retrievalUnitRow{}, &syncRunRow{}); err != nil {
		t.Fatalf("migrate mirror tables: %v", err)
	}
	store := NewGormStore(db)
	ctx := context.Background()
	for _, snapshot := range []Snapshot{
		latestOnlyTestSnapshot("record-a", "asset-a", "revision-a", "biology"),
		latestOnlyTestSnapshot("record-b", "asset-b", "revision-b", "history"),
		latestOnlyTestSnapshot("record-c", "asset-c", "revision-c", "physics"),
	} {
		if err := store.UpsertSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("upsert projection: %v", err)
		}
	}

	rows, total, err := store.ListMirrors(ctx, 2, 1, MirrorListFilter{})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if total != 3 || len(rows) != 1 {
		t.Fatalf("page result = total %d, rows %d; want total 3 and one row", total, len(rows))
	}

	rows, total, err = store.ListMirrors(ctx, 1, 100, MirrorListFilter{Query: "physics"})
	if err != nil {
		t.Fatalf("filter mirrors: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].Book.Title != "Book 1" {
		t.Fatalf("filtered result = total %d, rows %#v; want the physics book", total, rows)
	}
}

func TestSnapshotAllowsEmptyMediaRelationsForTextOnlyProjection(t *testing.T) {
	snapshot := latestOnlyTestSnapshot("record-text", "asset-text", "revision-1", "text")
	snapshot.Sidecars[ArtifactKindMediaRelations] = nil
	if err := snapshot.validate(); err != nil {
		t.Fatalf("text-only projection should allow empty media_relations: %v", err)
	}
}

func latestOnlyTestSnapshot(recordID, assetID, revisionKey, suffix string) Snapshot {
	unit, _ := json.Marshal(map[string]any{
		"unit_id":            "unit-" + suffix,
		"ordinal":            1,
		"structural_node_id": "node-" + suffix,
		"content_type":       "paragraph",
		"content_text":       "content-" + suffix,
		"locator":            map[string]any{"page": 1},
	})
	return Snapshot{
		ArchiveRecordID: recordID,
		AssetID:         assetID,
		SourceRef:       "fms:archive:" + recordID + ":asset:" + assetID,
		Book:            BookMetadata{BookNo: "book-" + suffix, Title: "Book 1", Subject: suffix, StandardClassification: &StandardClassification{PrimaryCode: "F", RelatedSecondaryCodes: []string{}}},
		Revision: SourceRevision{
			SourceChecksumSHA256: "checksum-" + suffix,
			ParserVersion:        "parser-" + suffix,
			SegmentPolicyVersion: "segment-v1",
			EmbeddingPolicy:      "embedding-v1",
			RevisionKey:          revisionKey,
		},
		ProjectionState: "ready", ReadinessStatus: "pass", HandoffEligible: true,
		Sidecars: map[string][]json.RawMessage{
			ArtifactKindDirectory:            {json.RawMessage(`{"ok":true}`)},
			ArtifactKindCanonical:            {json.RawMessage(`{"text":"content-` + suffix + `"}`)},
			ArtifactKindRetrievalUnits:       {unit},
			ArtifactKindMediaRelations:       {json.RawMessage(`{"relations":[]}`)},
			ArtifactKindSegmentationManifest: {json.RawMessage(`{"ok":true}`)},
		},
	}
}
