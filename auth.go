package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type ctxKey int

const tenantCtxKey ctxKey = iota

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

func adminAuth(cfg *Config, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "MISSING_TOKEN", "missing Authorization: Bearer <token> header")
			return
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Admin.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "INVALID_ADMIN_TOKEN", "invalid admin token")
			return
		}
		next(w, r)
	}
}

func tenantAuth(db *DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "MISSING_TOKEN", "missing Authorization: Bearer <token> header")
			return
		}
		tenant, err := db.AuthenticateTenant(r.Context(), token)
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "INVALID_API_KEY", "invalid or revoked API key")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to authenticate")
			return
		}
		ctx := context.WithValue(r.Context(), tenantCtxKey, tenant)
		next(w, r.WithContext(ctx))
	}
}

func tenantFromContext(ctx context.Context) *Tenant {
	t, _ := ctx.Value(tenantCtxKey).(*Tenant)
	return t
}
