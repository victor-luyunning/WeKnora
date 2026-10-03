package fmsbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
)

// Adapter reads published FMS knowledge projections. It has no write method
// by design: FMS remains the source of truth for authorization, publications,
// generations, and evidence locators.
type Adapter struct {
	config Config
}

func NewAdapter(config Config) (*Adapter, error) {
	normalized, err := config.normalized()
	if err != nil {
		return nil, err
	}
	return &Adapter{config: normalized}, nil
}

// ListProjections lists one authorized FMS page. The adapter does not treat
// the list's lightweight state as admission; each item must be re-read through
// the detail API before a mirror write is allowed.
func (a *Adapter) ListProjections(ctx context.Context, pageNumber int) ([]ProjectionRef, pagination, error) {
	if pageNumber < 1 {
		return nil, pagination{}, fmt.Errorf("FMS projection page must be positive")
	}
	var data struct {
		Items []ProjectionRef `json:"items"`
	}
	meta, err := a.get(ctx, "/api/v1/knowledge-projections", url.Values{
		"page":  []string{strconv.Itoa(pageNumber)},
		"limit": []string{strconv.Itoa(a.config.PageSize)},
	}, &data)
	if err != nil {
		return nil, pagination{}, err
	}
	for _, item := range data.Items {
		if err := item.validate(); err != nil {
			return nil, pagination{}, err
		}
	}
	return data.Items, meta, nil
}

func (a *Adapter) GetProjection(ctx context.Context, ref ProjectionRef) (projectionDetail, error) {
	if err := ref.validate(); err != nil {
		return projectionDetail{}, err
	}
	var detail projectionDetail
	_, err := a.get(ctx, path.Join("/api/v1/knowledge-projections", ref.ArchiveRecordID), url.Values{
		"asset_id": []string{ref.AssetID},
	}, &detail)
	if err != nil {
		return projectionDetail{}, err
	}
	if detail.ArchiveRecordID != ref.ArchiveRecordID || detail.AssetID != ref.AssetID {
		return projectionDetail{}, fmt.Errorf("FMS projection detail identity does not match requested item")
	}
	return detail, nil
}

func (a *Adapter) FetchProjection(ctx context.Context, ref ProjectionRef) (Snapshot, bool, error) {
	detail, err := a.GetProjection(ctx, ref)
	if err != nil {
		return Snapshot{}, false, err
	}
	packageData := detail.FutureSync
	if !packageData.HandoffEligible {
		return Snapshot{}, false, nil
	}
	if packageData.ProjectionState != "ready" {
		return Snapshot{}, false, fmt.Errorf("FMS marked an eligible projection as %q", packageData.ProjectionState)
	}
	if packageData.SourceRef == "" || packageData.SourceRevision.RevisionKey == "" {
		return Snapshot{}, false, fmt.Errorf("eligible FMS projection returned an incomplete source revision")
	}

	snapshot := Snapshot{
		ArchiveRecordID: detail.ArchiveRecordID,
		AssetID:         detail.AssetID,
		SourceRef:       packageData.SourceRef,
		Book:            packageData.Book,
		Revision:        packageData.SourceRevision,
		ProjectionState: packageData.ProjectionState,
		ReadinessStatus: packageData.ReadinessStatus,
		HandoffEligible: packageData.HandoffEligible,
		Artifacts:       packageData.Artifacts,
		Sidecars:        make(map[string][]json.RawMessage, len(RequiredArtifactKinds)),
	}
	if err := ensureRequiredArtifactsAvailable(packageData.Artifacts); err != nil {
		return Snapshot{}, false, err
	}
	for _, kind := range RequiredArtifactKinds {
		items, err := a.FetchArtifact(ctx, ref, kind)
		if err != nil {
			return Snapshot{}, false, fmt.Errorf("fetch %s for %s: %w", kind, packageData.SourceRef, err)
		}
		snapshot.Sidecars[kind] = items
	}
	if err := snapshot.validate(); err != nil {
		return Snapshot{}, false, err
	}
	return snapshot, true, nil
}

// FetchAllEligible is useful to CLI callers. Reconciler should be preferred
// for a production run because it records item-level failures and continues.
func (a *Adapter) FetchAllEligible(ctx context.Context) ([]Snapshot, error) {
	pageNumber := 1
	var snapshots []Snapshot
	for {
		items, meta, err := a.ListProjections(ctx, pageNumber)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			snapshot, eligible, err := a.FetchProjection(ctx, item)
			if err != nil {
				return nil, err
			}
			if eligible {
				snapshots = append(snapshots, snapshot)
			}
		}
		if !meta.HasNext {
			return snapshots, nil
		}
		pageNumber++
		if meta.TotalPages > 0 && pageNumber > meta.TotalPages {
			return nil, fmt.Errorf("FMS pagination reported has_next beyond total_pages")
		}
	}
}

func (a *Adapter) FetchArtifact(ctx context.Context, ref ProjectionRef, kind string) ([]json.RawMessage, error) {
	if err := ref.validate(); err != nil {
		return nil, err
	}
	if !isRequiredArtifactKind(kind) {
		return nil, fmt.Errorf("artifact kind %q is not a required FMS bridge input", kind)
	}
	pageNumber := 1
	var records []json.RawMessage
	for {
		var data struct {
			ArchiveRecordID string            `json:"archive_record_id"`
			AssetID         string            `json:"asset_id"`
			Artifact        Artifact          `json:"artifact"`
			Items           []json.RawMessage `json:"items"`
			Pagination      pagination        `json:"pagination"`
		}
		_, err := a.get(ctx, path.Join("/api/v1/knowledge-projections", ref.ArchiveRecordID, "artifact-preview"), url.Values{
			"asset_id": []string{ref.AssetID},
			"kind":     []string{kind},
			"page":     []string{strconv.Itoa(pageNumber)},
			"limit":    []string{strconv.Itoa(a.config.ArtifactPageSize)},
		}, &data)
		if err != nil {
			return nil, err
		}
		if data.ArchiveRecordID != ref.ArchiveRecordID || data.AssetID != ref.AssetID || data.Artifact.Kind != kind {
			return nil, fmt.Errorf("FMS artifact preview identity does not match requested sidecar")
		}
		if !data.Artifact.Required || !data.Artifact.Available {
			return nil, fmt.Errorf("FMS required sidecar %q is unavailable", kind)
		}
		records = append(records, data.Items...)
		if data.Pagination.TotalPages == 0 || pageNumber >= data.Pagination.TotalPages {
			return records, nil
		}
		pageNumber++
	}
}

func (a *Adapter) get(ctx context.Context, endpoint string, query url.Values, target any) (pagination, error) {
	requestURL := a.config.BaseURL + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return pagination{}, err
	}
	req.Header.Set("Accept", "application/json")
	if a.config.ServiceToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.ServiceToken)
	}
	response, err := a.config.HTTPClient.Do(req)
	if err != nil {
		return pagination{}, fmt.Errorf("FMS request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return pagination{}, fmt.Errorf("read FMS response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return pagination{}, fmt.Errorf("FMS request returned HTTP %d", response.StatusCode)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return pagination{}, fmt.Errorf("decode FMS response: %w", err)
	}
	if !envelope.Success || envelope.Code != "OK" {
		return pagination{}, fmt.Errorf("FMS request was rejected: %s", envelope.Message)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return pagination{}, fmt.Errorf("decode FMS response data: %w", err)
	}
	return envelope.Meta.Pagination, nil
}

func ensureRequiredArtifactsAvailable(artifacts []Artifact) error {
	byKind := make(map[string]Artifact, len(artifacts))
	for _, artifact := range artifacts {
		byKind[artifact.Kind] = artifact
	}
	for _, kind := range RequiredArtifactKinds {
		artifact, ok := byKind[kind]
		if !ok || !artifact.Required || !artifact.Available {
			return fmt.Errorf("FMS required sidecar %q is unavailable", kind)
		}
	}
	return nil
}

func isRequiredArtifactKind(kind string) bool {
	for _, candidate := range RequiredArtifactKinds {
		if kind == candidate {
			return true
		}
	}
	return false
}
