package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/fmsbridge"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// FMSBridgeHandler exposes the deliberately narrow, platform-admin-only
// trigger for the read-only FMS projection mirror.
type FMSBridgeHandler struct {
	service *fmsbridge.RuntimeService
}

func NewFMSBridgeHandler(service *fmsbridge.RuntimeService) *FMSBridgeHandler {
	return &FMSBridgeHandler{service: service}
}

func (h *FMSBridgeHandler) Reconcile(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "FMS bridge is unavailable"})
		return
	}
	info, err := h.service.EnqueueReconcile(c.Request.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, fmsbridge.ErrDisabled) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{
		"task_id": info.ID, "status": "queued",
	}})
}

func (h *FMSBridgeHandler) ReconcileMirror(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "FMS bridge is unavailable"})
		return
	}
	info, err := h.service.EnqueueMirrorReconcile(c.Request.Context(), c.Param("id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "FMS mirror not found"})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, fmsbridge.ErrDisabled) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{
		"task_id": info.ID, "status": "queued",
	}})
}

// Overview returns the local, rebuildable FMS mirror state. It is deliberately
// separate from FMS itself: this endpoint never triggers a remote request.
func (h *FMSBridgeHandler) Overview(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "FMS bridge is unavailable"})
		return
	}
	page, pageSize := bridgePagination(c)
	handoffEligible, validHandoffEligible := bridgeBoolFilter(c, "handoff_eligible")
	if !validHandoffEligible {
		c.JSON(http.StatusBadRequest, gin.H{"error": "handoff_eligible must be true or false"})
		return
	}
	overview, err := h.service.Overview(c.Request.Context(), page, pageSize, fmsbridge.MirrorListFilter{
		Query:           strings.TrimSpace(c.Query("query")),
		ReadinessStatus: strings.TrimSpace(c.Query("readiness_status")),
		HandoffEligible: handoffEligible,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read FMS bridge overview"})
		return
	}
	c.JSON(http.StatusOK, overview)
}

// MirrorDetail exposes a bounded retrieval-unit preview for one already
// mirrored source version. It does not call FMS or reveal FMS storage paths.
func (h *FMSBridgeHandler) MirrorDetail(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "FMS bridge is unavailable"})
		return
	}
	detail, err := h.service.MirrorDetail(c.Request.Context(), c.Param("id"), bridgeLimit(c, 50))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "FMS mirror not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read FMS mirror"})
		return
	}
	c.JSON(http.StatusOK, detail)
}

func bridgeLimit(c *gin.Context, fallback int) int {
	value := c.Query("limit")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > 100 {
		return fallback
	}
	return parsed
}

func bridgePagination(c *gin.Context) (page, pageSize int) {
	page, pageSize = 1, 100
	if value := c.Query("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if value := c.Query("page_size"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 1 && parsed <= 100 {
			pageSize = parsed
		}
	}
	return page, pageSize
}

func bridgeBoolFilter(c *gin.Context, key string) (*bool, bool) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return nil, true
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, false
	}
	return &parsed, true
}
