package main

import (
	"errors"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type opsHandlers struct {
	db     *DB
	s3     *S3Client
	cfg    *Config
	maxTTL time.Duration
	defTTL time.Duration
}

func newOpsHandlers(db *DB, s3 *S3Client, cfg *Config) *opsHandlers {
	return &opsHandlers{
		db:     db,
		s3:     s3,
		cfg:    cfg,
		maxTTL: time.Duration(cfg.TTL.MaxSeconds) * time.Second,
		defTTL: time.Duration(cfg.TTL.DefaultSeconds) * time.Second,
	}
}

type uploadResponse struct {
	Key       string `json:"key"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (h *opsHandlers) upload(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromContext(r.Context())

	maxBodyBytes := h.cfg.Limits.MaxFileSizeMB*1024*1024 + 64*1024 // small headroom for form overhead
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "file exceeds max_file_size_mb or body malformed")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "MISSING_FILE", "multipart field 'file' is required")
		return
	}
	defer file.Close()

	ttl := h.defTTL
	if v := r.FormValue("ttl_seconds"); v != "" {
		secs, err := strconv.ParseInt(v, 10, 64)
		if err != nil || secs <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_TTL", "ttl_seconds must be a positive integer")
			return
		}
		ttl = time.Duration(secs) * time.Second
		if ttl > h.maxTTL {
			ttl = h.maxTTL
		}
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	key := uuid.NewString() + ext

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if err := h.s3.Upload(r.Context(), key, contentType, file, header.Size); err != nil {
		log.Printf("upload to s3 failed: %v", err)
		writeError(w, http.StatusInternalServerError, "UPLOAD_FAILED", "failed to store file")
		return
	}

	expiresAt := time.Now().Add(ttl)
	_, err = h.db.CreateAsset(r.Context(), Asset{
		Key:              key,
		TenantID:         tenant.ID,
		OriginalFilename: header.Filename,
		ContentType:      contentType,
		SizeBytes:        header.Size,
		ExpiresAt:        expiresAt,
	})
	if err != nil {
		log.Printf("record asset failed: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "file uploaded but failed to record metadata")
		return
	}

	writeJSON(w, http.StatusCreated, uploadResponse{
		Key:       key,
		URL:       strings.TrimRight(h.cfg.Server.BaseURL, "/") + "/" + key,
		ExpiresAt: expiresAt.Format(timeFormat),
	})
}

type assetResponse struct {
	Key              string `json:"key"`
	URL              string `json:"url"`
	OriginalFilename string `json:"original_filename"`
	ContentType      string `json:"content_type"`
	SizeBytes        int64  `json:"size_bytes"`
	CreatedAt        string `json:"created_at"`
	ExpiresAt        string `json:"expires_at"`
	Expired          bool   `json:"expired"`
}

func (h *opsHandlers) getAsset(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromContext(r.Context())
	key := r.PathValue("key")

	a, err := h.db.GetOwnedAsset(r.Context(), tenant.ID, key)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "ASSET_NOT_FOUND", "asset not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch asset")
		return
	}

	writeJSON(w, http.StatusOK, assetResponse{
		Key:              a.Key,
		URL:              strings.TrimRight(h.cfg.Server.BaseURL, "/") + "/" + a.Key,
		OriginalFilename: a.OriginalFilename,
		ContentType:      a.ContentType,
		SizeBytes:        a.SizeBytes,
		CreatedAt:        a.CreatedAt.Format(timeFormat),
		ExpiresAt:        a.ExpiresAt.Format(timeFormat),
		Expired:          a.ExpiresAt.Before(time.Now()),
	})
}

func (h *opsHandlers) deleteAsset(w http.ResponseWriter, r *http.Request) {
	tenant := tenantFromContext(r.Context())
	key := r.PathValue("key")

	a, err := h.db.GetOwnedAsset(r.Context(), tenant.ID, key)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "ASSET_NOT_FOUND", "asset not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to fetch asset")
		return
	}

	if err := h.s3.Delete(r.Context(), a.Key); err != nil {
		log.Printf("delete from s3 failed: %v", err)
		writeError(w, http.StatusInternalServerError, "DELETE_FAILED", "failed to delete file")
		return
	}
	if err := h.db.MarkAssetDeleted(r.Context(), a.ID); err != nil {
		log.Printf("mark asset deleted failed: %v", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "file deleted but failed to update metadata")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
