package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type adminHandlers struct {
	db *DB
}

type createTenantRequest struct {
	Name string `json:"name"`
}

type createTenantResponse struct {
	TenantID int64  `json:"tenant_id"`
	Name     string `json:"name"`
	APIKey   string `json:"api_key"`
}

func (h *adminHandlers) createTenant(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "body must be JSON: {\"name\": \"...\"}")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "MISSING_NAME", "field 'name' is required")
		return
	}

	tenant, apiKey, err := h.db.CreateTenant(r.Context(), req.Name)
	if err != nil {
		writeError(w, http.StatusConflict, "TENANT_CREATE_FAILED", "tenant name may already exist")
		return
	}

	writeJSON(w, http.StatusCreated, createTenantResponse{
		TenantID: tenant.ID,
		Name:     tenant.Name,
		APIKey:   apiKey,
	})
}

type tenantSummary struct {
	TenantID  int64  `json:"tenant_id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
}

func (h *adminHandlers) listTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.db.ListTenants(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list tenants")
		return
	}

	out := make([]tenantSummary, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, tenantSummary{
			TenantID:  t.ID,
			Name:      t.Name,
			Active:    t.DeletedAt == nil,
			CreatedAt: t.CreatedAt.Format(timeFormat),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
}

func (h *adminHandlers) revokeTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "tenant id must be numeric")
		return
	}
	if err := h.db.RevokeTenant(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "tenant not found or already revoked")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to revoke tenant")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *adminHandlers) regenerateKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ID", "tenant id must be numeric")
		return
	}
	apiKey, err := h.db.RegenerateAPIKey(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "TENANT_NOT_FOUND", "tenant not found or revoked")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to regenerate key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": apiKey})
}
