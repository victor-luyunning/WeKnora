// Package fmsbridge is the read-only boundary between WeKnora-FMS and FMS.
//
// It deliberately consumes only FMS's published knowledge-projection API.
// It must never read FMS tables, storage paths, object keys, or source EPUBs;
// the generic WeKnora datasource pipeline is intentionally not used here
// because that pipeline reparses documents and may schedule indexing work.
package fmsbridge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ArtifactKindDirectory            = "directory"
	ArtifactKindCanonical            = "canonical"
	ArtifactKindRetrievalUnits       = "retrieval_units"
	ArtifactKindMediaRelations       = "media_relations"
	ArtifactKindSegmentationManifest = "segmentation_manifest"
)

// RequiredArtifactKinds is the FMS contract's complete body projection. OCR
// intentionally does not appear here: it is a non-blocking optional sidecar.
// media_relations is required as a published sidecar, but may contain zero
// records when FMS has no images or other media for the projection.
var RequiredArtifactKinds = []string{
	ArtifactKindDirectory,
	ArtifactKindCanonical,
	ArtifactKindRetrievalUnits,
	ArtifactKindMediaRelations,
	ArtifactKindSegmentationManifest,
}

// Config is deployment-owned source configuration. The service token is only
// ever sent as a Bearer header and is never copied into a Snapshot or log.
type Config struct {
	BaseURL      string
	ServiceToken string
	HTTPClient   *http.Client
	PageSize     int
	Timeout      time.Duration
}

func (c Config) normalized() (Config, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return Config{}, fmt.Errorf("FMS base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Config{}, fmt.Errorf("invalid FMS base URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Config{}, fmt.Errorf("FMS base URL must use http or https")
	}
	c.BaseURL = baseURL
	c.ServiceToken = strings.TrimSpace(c.ServiceToken)
	if c.ServiceToken == "" {
		return Config{}, fmt.Errorf("FMS service token is required")
	}
	if c.PageSize <= 0 {
		c.PageSize = 100
	}
	if c.PageSize > 100 {
		return Config{}, fmt.Errorf("FMS page size must not exceed 100")
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: c.Timeout}
	}
	return c, nil
}

// ProjectionRef is the only locator obtained from an FMS list response.
type ProjectionRef struct {
	ArchiveRecordID string `json:"archive_record_id"`
	AssetID         string `json:"asset_id"`
}

func (r ProjectionRef) validate() error {
	if r.ArchiveRecordID == "" || r.AssetID == "" {
		return fmt.Errorf("FMS projection list item is missing archive_record_id or asset_id")
	}
	if !safePathIdentifier(r.ArchiveRecordID) || !safePathIdentifier(r.AssetID) {
		return fmt.Errorf("FMS projection identifier contains unsupported characters")
	}
	return nil
}

func safePathIdentifier(value string) bool {
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

type pagination struct {
	Page       int  `json:"page"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
}

type responseEnvelope struct {
	Success bool            `json:"success"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Meta    struct {
		Pagination pagination `json:"pagination"`
	} `json:"meta"`
}

// Artifact describes a FMS logical sidecar; no storage location is present.
type Artifact struct {
	Kind        string `json:"kind"`
	Required    bool   `json:"required"`
	Available   bool   `json:"available"`
	SHA256      string `json:"sha256"`
	RecordCount *int   `json:"record_count"`
}

type SourceRevision struct {
	SourceChecksumSHA256 string `json:"source_checksum_sha256"`
	ParserVersion        string `json:"parser_version"`
	SegmentPolicyVersion string `json:"segment_policy_version"`
	EmbeddingPolicy      string `json:"embedding_context_policy_version"`
	RevisionKey          string `json:"revision_key"`
}

type FutureSyncPackage struct {
	SourceRef       string         `json:"source_ref"`
	Book            BookMetadata   `json:"book"`
	SourceRevision  SourceRevision `json:"source_revision"`
	ProjectionState string         `json:"projection_state"`
	ReadinessStatus string         `json:"readiness_status"`
	HandoffEligible bool           `json:"weknora_handoff_eligible"`
	Artifacts       []Artifact     `json:"artifacts"`
}

// BookMetadata is the small FMS-published catalogue summary that makes an
// administrative mirror understandable without duplicating FMS's book model.
type BookMetadata struct {
	BookNo  string `json:"book_no"`
	Title   string `json:"title"`
	Subject string `json:"subject"`
}

type projectionDetail struct {
	ArchiveRecordID string            `json:"archive_record_id"`
	AssetID         string            `json:"asset_id"`
	FutureSync      FutureSyncPackage `json:"future_sync_package"`
}

// Snapshot is a fully fetched immutable source version ready to be committed
// atomically by a mirror store. Sidecars retain FMS's JSON values but contain
// no FMS-private storage references.
type Snapshot struct {
	ArchiveRecordID string
	AssetID         string
	SourceRef       string
	Book            BookMetadata
	Revision        SourceRevision
	ProjectionState string
	ReadinessStatus string
	HandoffEligible bool
	Artifacts       []Artifact
	Sidecars        map[string][]json.RawMessage
}

// BridgeOverview is safe for the WeKnora system-administration UI. It never
// exposes the FMS base URL, token, storage location, or source files.
type BridgeOverview struct {
	Enabled            bool            `json:"enabled"`
	ConfigurationIssue string          `json:"configuration_issue,omitempty"`
	MirrorCount        int64           `json:"mirror_count"`
	LatestRun          *SyncRunSummary `json:"latest_run,omitempty"`
	Mirrors            []MirrorSummary `json:"mirrors"`
}

type MirrorSummary struct {
	ID                 string       `json:"id"`
	SourceRef          string       `json:"source_ref"`
	RevisionKey        string       `json:"revision_key"`
	ArchiveRecordID    string       `json:"archive_record_id"`
	AssetID            string       `json:"asset_id"`
	Book               BookMetadata `json:"book"`
	ProjectionState    string       `json:"projection_state"`
	ReadinessStatus    string       `json:"readiness_status"`
	HandoffEligible    bool         `json:"handoff_eligible"`
	RetrievalUnitCount int64        `json:"retrieval_unit_count"`
	LastSyncedAt       time.Time    `json:"last_synced_at"`
}

type MirrorDetail struct {
	MirrorSummary
	Artifacts      []Artifact             `json:"artifacts"`
	RetrievalUnits []RetrievalUnitPreview `json:"retrieval_units"`
}

type RetrievalUnitPreview struct {
	UnitID           string          `json:"unit_id"`
	Ordinal          int             `json:"ordinal"`
	StructuralNodeID string          `json:"structural_node_id"`
	ContentType      string          `json:"content_type"`
	ContentText      string          `json:"content_text"`
	Locator          json.RawMessage `json:"locator"`
}

type SyncRunSummary struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   *time.Time     `json:"finished_at,omitempty"`
	Summary      SyncRunMetrics `json:"summary"`
	ErrorMessage string         `json:"error_message,omitempty"`
}

type SyncRunMetrics struct {
	Seen      int               `json:"seen"`
	Succeeded int               `json:"succeeded"`
	Skipped   int               `json:"skipped"`
	Failed    int               `json:"failed"`
	Failures  map[string]string `json:"failures,omitempty"`
}

func (s Snapshot) validate() error {
	if err := (ProjectionRef{ArchiveRecordID: s.ArchiveRecordID, AssetID: s.AssetID}).validate(); err != nil {
		return err
	}
	if s.SourceRef == "" || s.Revision.RevisionKey == "" || s.Revision.SourceChecksumSHA256 == "" {
		return fmt.Errorf("eligible FMS projection is missing source_ref, revision_key, or source checksum")
	}
	if !s.HandoffEligible || s.ProjectionState != "ready" {
		return fmt.Errorf("snapshot is not eligible for handoff")
	}
	for _, kind := range RequiredArtifactKinds {
		if kind != ArtifactKindMediaRelations && len(s.Sidecars[kind]) == 0 {
			return fmt.Errorf("required FMS sidecar %q is empty", kind)
		}
	}
	return nil
}

// SyncResult is deliberately per-run and does not infer source deletion from a
// failed or interrupted list traversal. Tombstone/change-feed support is a
// later FMS contract phase.
type SyncResult struct {
	Seen      int
	Succeeded int
	Skipped   int
	Failed    int
	Failures  map[string]string
}
