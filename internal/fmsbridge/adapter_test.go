package fmsbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAdapterFetchesEligibleProjectionAndAllRequiredSidecars(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.String())
		if got := r.Header.Get("Authorization"); got != "Bearer service-token" {
			t.Fatalf("Authorization = %q", got)
		}

		switch r.URL.Path {
		case "/api/v1/knowledge-projections":
			page := r.URL.Query().Get("page")
			if page == "1" {
				writeFMSResponse(t, w, map[string]any{"items": []any{
					map[string]any{"archive_record_id": "record-pass", "asset_id": "asset-pass"},
				}}, map[string]any{"pagination": map[string]any{"page": 1, "total_pages": 2, "has_next": true}})
				return
			}
			writeFMSResponse(t, w, map[string]any{"items": []any{
				map[string]any{"archive_record_id": "record-review", "asset_id": "asset-review"},
			}}, map[string]any{"pagination": map[string]any{"page": 2, "total_pages": 2, "has_next": false}})
		case "/api/v1/knowledge-projections/record-pass":
			writeProjectionDetail(t, w, "record-pass", "asset-pass", false, "pass")
		case "/api/v1/knowledge-projections/record-review":
			writeProjectionDetail(t, w, "record-review", "asset-review", true, "review")
		case "/api/v1/knowledge-projections/record-review/artifact-preview":
			kind := r.URL.Query().Get("kind")
			page := r.URL.Query().Get("page")
			if kind == "retrieval_units" && page == "1" {
				writeFMSResponse(t, w, map[string]any{
					"archive_record_id": "record-review",
					"asset_id":          "asset-review",
					"artifact":          map[string]any{"kind": kind, "required": true, "available": true},
					"items":             []any{map[string]any{"unit_id": "u-1", "content_text": "第一页"}},
					"pagination":        map[string]any{"page": 1, "total_pages": 2},
				}, nil)
				return
			}
			writeFMSResponse(t, w, map[string]any{
				"archive_record_id": "record-review",
				"asset_id":          "asset-review",
				"artifact":          map[string]any{"kind": kind, "required": true, "available": true},
				"items":             []any{map[string]any{"kind": kind, "page": page}},
				"pagination":        map[string]any{"page": 2, "total_pages": 2},
			}, nil)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter, err := NewAdapter(Config{BaseURL: server.URL, ServiceToken: "service-token", PageSize: 1})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	snapshots, err := adapter.FetchAllEligible(context.Background())
	if err != nil {
		t.Fatalf("FetchAllEligible() error = %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want 1", len(snapshots))
	}
	snapshot := snapshots[0]
	if snapshot.SourceRef != "fms:archive:record-review:asset:asset-review" {
		t.Fatalf("SourceRef = %q", snapshot.SourceRef)
	}
	if snapshot.ReadinessStatus != "review" || !snapshot.HandoffEligible {
		t.Fatalf("review projection was not admitted: %#v", snapshot)
	}
	if got := len(snapshot.Sidecars[ArtifactKindRetrievalUnits]); got != 2 {
		t.Fatalf("retrieval_units pages = %d, want 2", got)
	}
	if !containsRequest(requests, "/api/v1/knowledge-projections/record-pass?asset_id=asset-pass") {
		t.Fatalf("ineligible projection detail was not checked: %v", requests)
	}
}

func TestReconcilerKeepsExistingMirrorWhenRequiredSidecarFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/knowledge-projections":
			writeFMSResponse(t, w, map[string]any{"items": []any{
				map[string]any{"archive_record_id": "record-1", "asset_id": "asset-1"},
			}}, map[string]any{"pagination": map[string]any{"page": 1, "total_pages": 1, "has_next": false}})
		case "/api/v1/knowledge-projections/record-1":
			writeProjectionDetail(t, w, "record-1", "asset-1", true, "pass")
		case "/api/v1/knowledge-projections/record-1/artifact-preview":
			if r.URL.Query().Get("kind") == ArtifactKindMediaRelations {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"success":false,"code":"upstream_error","message":"unavailable"}`))
				return
			}
			writeFMSResponse(t, w, map[string]any{
				"archive_record_id": "record-1",
				"asset_id":          "asset-1",
				"artifact":          map[string]any{"kind": r.URL.Query().Get("kind"), "required": true, "available": true},
				"items":             []any{map[string]any{"ok": true}},
				"pagination":        map[string]any{"page": 1, "total_pages": 1},
			}, nil)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter, err := NewAdapter(Config{BaseURL: server.URL, ServiceToken: "service-token", PageSize: 20})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	store := &recordingStore{}
	reconciler := NewReconciler(adapter, store)

	result, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(store.snapshots) != 0 {
		t.Fatalf("failed source must not replace existing mirror: %#v", store.snapshots)
	}
}

func TestReconcilerReconcileOneFetchesOnlyRequestedProjection(t *testing.T) {
	requested := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		switch r.URL.Path {
		case "/api/v1/knowledge-projections/record-target":
			writeProjectionDetail(t, w, "record-target", "asset-target", true, "review")
		case "/api/v1/knowledge-projections/record-target/artifact-preview":
			writeFMSResponse(t, w, map[string]any{
				"archive_record_id": "record-target",
				"asset_id":          "asset-target",
				"artifact":          map[string]any{"kind": r.URL.Query().Get("kind"), "required": true, "available": true},
				"items":             []any{map[string]any{"kind": r.URL.Query().Get("kind")}},
				"pagination":        map[string]any{"page": 1, "total_pages": 1},
			}, nil)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter, err := NewAdapter(Config{BaseURL: server.URL, ServiceToken: "service-token", PageSize: 20})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	store := &recordingStore{}
	result, err := NewReconciler(adapter, store).ReconcileOne(context.Background(), ProjectionRef{
		ArchiveRecordID: "record-target", AssetID: "asset-target",
	})
	if err != nil {
		t.Fatalf("ReconcileOne() error = %v", err)
	}
	if result.Seen != 1 || result.Succeeded != 1 || result.Failed != 0 || len(store.snapshots) != 1 {
		t.Fatalf("result = %#v snapshots = %#v", result, store.snapshots)
	}
	if requested != "/api/v1/knowledge-projections/record-target/artifact-preview" {
		t.Fatalf("last requested path = %q", requested)
	}
}

type recordingStore struct {
	snapshots []Snapshot
}

func (s *recordingStore) UpsertSnapshot(_ context.Context, snapshot Snapshot) error {
	s.snapshots = append(s.snapshots, snapshot)
	return nil
}

func writeProjectionDetail(t *testing.T, w http.ResponseWriter, recordID, assetID string, eligible bool, readiness string) {
	t.Helper()
	artifacts := make([]any, 0, len(RequiredArtifactKinds))
	for _, kind := range RequiredArtifactKinds {
		artifacts = append(artifacts, map[string]any{"kind": kind, "required": true, "available": true})
	}
	writeFMSResponse(t, w, map[string]any{
		"archive_record_id":        recordID,
		"asset_id":                 assetID,
		"projection_state":         "ready",
		"readiness_status":         readiness,
		"weknora_handoff_eligible": eligible,
		"future_sync_package": map[string]any{
			"source_ref":               "fms:archive:" + recordID + ":asset:" + assetID,
			"projection_state":         "ready",
			"readiness_status":         readiness,
			"weknora_handoff_eligible": eligible,
			"source_revision": map[string]any{
				"source_checksum_sha256": "checksum-" + recordID,
				"revision_key":           "revision-" + recordID,
			},
			"artifacts": artifacts,
		},
	}, nil)
}

func writeFMSResponse(t *testing.T, w http.ResponseWriter, data any, meta any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]any{"success": true, "code": "OK", "data": data, "meta": meta, "error": nil}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func containsRequest(requests []string, want string) bool {
	for _, request := range requests {
		parsed, err := url.Parse(request)
		if err != nil {
			continue
		}
		if parsed.Path+"?"+parsed.RawQuery == want {
			return true
		}
	}
	return false
}
