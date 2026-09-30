package fmsbridge

import (
	"context"
	"fmt"
)

// MirrorStore commits a complete Snapshot atomically. An implementation must
// not delete or replace the current projection item until every required FMS
// sidecar for the incoming revision has been fetched and validated.
type MirrorStore interface {
	UpsertSnapshot(ctx context.Context, snapshot Snapshot) error
}

type Reconciler struct {
	adapter *Adapter
	store   MirrorStore
}

func NewReconciler(adapter *Adapter, store MirrorStore) *Reconciler {
	return &Reconciler{adapter: adapter, store: store}
}

// Reconcile is an explicit full scan. It deliberately does not infer deletion
// from an incomplete run; FMS change feeds and tombstones are a later phase.
func (r *Reconciler) Reconcile(ctx context.Context) (result SyncResult, runErr error) {
	if r == nil || r.adapter == nil || r.store == nil {
		return SyncResult{}, fmt.Errorf("FMS reconciler is not configured")
	}
	result = SyncResult{Failures: make(map[string]string)}
	if recorder, ok := r.store.(runStore); ok {
		runID, err := recorder.BeginRun(ctx)
		if err != nil {
			return result, err
		}
		defer func() {
			if finishErr := recorder.FinishRun(ctx, runID, result, runErr); finishErr != nil && runErr == nil {
				runErr = finishErr
			}
		}()
	}
	pageNumber := 1
	for {
		items, meta, err := r.adapter.ListProjections(ctx, pageNumber)
		if err != nil {
			return result, err
		}
		for _, item := range items {
			result.Seen++
			r.reconcileProjection(ctx, item, &result)
		}
		if !meta.HasNext {
			return result, nil
		}
		pageNumber++
		if meta.TotalPages > 0 && pageNumber > meta.TotalPages {
			return result, fmt.Errorf("FMS pagination reported has_next beyond total_pages")
		}
	}
}

func (r *Reconciler) ReconcileOne(ctx context.Context, ref ProjectionRef) (result SyncResult, runErr error) {
	if r == nil || r.adapter == nil || r.store == nil {
		return SyncResult{}, fmt.Errorf("FMS reconciler is not configured")
	}
	result = SyncResult{Seen: 1, Failures: make(map[string]string)}
	if recorder, ok := r.store.(runStore); ok {
		runID, err := recorder.BeginRun(ctx)
		if err != nil {
			return result, err
		}
		defer func() {
			if finishErr := recorder.FinishRun(ctx, runID, result, runErr); finishErr != nil && runErr == nil {
				runErr = finishErr
			}
		}()
	}
	r.reconcileProjection(ctx, ref, &result)
	return result, nil
}

func (r *Reconciler) reconcileProjection(ctx context.Context, ref ProjectionRef, result *SyncResult) {
	snapshot, eligible, fetchErr := r.adapter.FetchProjection(ctx, ref)
	if fetchErr != nil {
		result.Failed++
		result.Failures["fms:archive:"+ref.ArchiveRecordID+":asset:"+ref.AssetID] = fetchErr.Error()
		return
	}
	if !eligible {
		result.Skipped++
		return
	}
	if err := r.store.UpsertSnapshot(ctx, snapshot); err != nil {
		result.Failed++
		result.Failures[snapshot.SourceRef] = err.Error()
		return
	}
	result.Succeeded++
}
