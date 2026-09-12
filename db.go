package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaSQL string

var ErrNotFound = errors.New("not found")

type DB struct {
	sql *sql.DB
}

func OpenDB(dsn string) (*DB, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &DB{sql: sqlDB}, nil
}

func (d *DB) Migrate(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, schemaSQL)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

type Tenant struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	DeletedAt *time.Time
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func generateAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// CreateTenant inserts a new tenant and returns the plaintext API key (shown once).
func (d *DB) CreateTenant(ctx context.Context, name string) (*Tenant, string, error) {
	apiKey, err := generateAPIKey()
	if err != nil {
		return nil, "", err
	}
	var t Tenant
	err = d.sql.QueryRowContext(ctx,
		`INSERT INTO tenants (name, api_key_hash) VALUES ($1, $2)
		 RETURNING id, name, created_at`,
		name, hashAPIKey(apiKey),
	).Scan(&t.ID, &t.Name, &t.CreatedAt)
	if err != nil {
		return nil, "", fmt.Errorf("create tenant: %w", err)
	}
	return &t, apiKey, nil
}

func (d *DB) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, name, created_at, deleted_at FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) RevokeTenant(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE tenants SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke tenant: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RegenerateAPIKey issues a new API key for an existing, active tenant.
func (d *DB) RegenerateAPIKey(ctx context.Context, id int64) (string, error) {
	apiKey, err := generateAPIKey()
	if err != nil {
		return "", err
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE tenants SET api_key_hash = $1 WHERE id = $2 AND deleted_at IS NULL`,
		hashAPIKey(apiKey), id)
	if err != nil {
		return "", fmt.Errorf("regenerate key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", ErrNotFound
	}
	return apiKey, nil
}

// AuthenticateTenant looks up the tenant owning apiKey. Returns ErrNotFound if
// the key is invalid or belongs to a revoked tenant.
func (d *DB) AuthenticateTenant(ctx context.Context, apiKey string) (*Tenant, error) {
	var t Tenant
	err := d.sql.QueryRowContext(ctx,
		`SELECT id, name, created_at, deleted_at FROM tenants
		 WHERE api_key_hash = $1 AND deleted_at IS NULL`,
		hashAPIKey(apiKey),
	).Scan(&t.ID, &t.Name, &t.CreatedAt, &t.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate tenant: %w", err)
	}
	return &t, nil
}

type Asset struct {
	ID               int64
	Key              string
	TenantID         int64
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	CreatedAt        time.Time
	ExpiresAt        time.Time
	DeletedAt        *time.Time
}

func (d *DB) CreateAsset(ctx context.Context, a Asset) (*Asset, error) {
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO assets (key, tenant_id, original_filename, content_type, size_bytes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		a.Key, a.TenantID, a.OriginalFilename, a.ContentType, a.SizeBytes, a.ExpiresAt,
	).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create asset: %w", err)
	}
	return &a, nil
}

// GetOwnedAsset returns the asset only if it belongs to tenantID and hasn't been deleted.
func (d *DB) GetOwnedAsset(ctx context.Context, tenantID int64, key string) (*Asset, error) {
	var a Asset
	err := d.sql.QueryRowContext(ctx,
		`SELECT id, key, tenant_id, original_filename, content_type, size_bytes, created_at, expires_at, deleted_at
		 FROM assets WHERE key = $1 AND tenant_id = $2 AND deleted_at IS NULL`,
		key, tenantID,
	).Scan(&a.ID, &a.Key, &a.TenantID, &a.OriginalFilename, &a.ContentType, &a.SizeBytes, &a.CreatedAt, &a.ExpiresAt, &a.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get asset: %w", err)
	}
	return &a, nil
}

func (d *DB) MarkAssetDeleted(ctx context.Context, id int64) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE assets SET deleted_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark asset deleted: %w", err)
	}
	return nil
}

func (d *DB) ListExpiredAssets(ctx context.Context, limit int) ([]Asset, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, key, tenant_id, original_filename, content_type, size_bytes, created_at, expires_at, deleted_at
		 FROM assets WHERE expires_at < now() AND deleted_at IS NULL
		 ORDER BY expires_at LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired assets: %w", err)
	}
	defer rows.Close()

	var out []Asset
	for rows.Next() {
		var a Asset
		if err := rows.Scan(&a.ID, &a.Key, &a.TenantID, &a.OriginalFilename, &a.ContentType, &a.SizeBytes, &a.CreatedAt, &a.ExpiresAt, &a.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan asset: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
