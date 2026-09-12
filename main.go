package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const timeFormat = time.RFC3339

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: asset-api <serve|worker>")
		os.Exit(1)
	}
	mode := os.Args[1]

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := OpenDB(cfg.Database.DSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate db: %v", err)
	}

	s3Client, err := NewS3Client(ctx, cfg)
	if err != nil {
		log.Fatalf("init s3 client: %v", err)
	}

	switch mode {
	case "serve":
		runServer(ctx, cfg, db, s3Client)
	case "worker":
		runWorker(ctx, cfg, db, s3Client)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q, expected 'serve' or 'worker'\n", mode)
		os.Exit(1)
	}
}

func runServer(ctx context.Context, cfg *Config, db *DB, s3Client *S3Client) {
	admin := &adminHandlers{db: db}
	ops := newOpsHandlers(db, s3Client, cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("POST /api/admin/tenants", adminAuth(cfg, admin.createTenant))
	mux.HandleFunc("GET /api/admin/tenants", adminAuth(cfg, admin.listTenants))
	mux.HandleFunc("DELETE /api/admin/tenants/{id}", adminAuth(cfg, admin.revokeTenant))
	mux.HandleFunc("POST /api/admin/tenants/{id}/regenerate", adminAuth(cfg, admin.regenerateKey))

	mux.HandleFunc("POST /api/ops/upload", tenantAuth(db, ops.upload))
	mux.HandleFunc("GET /api/ops/assets/{key}", tenantAuth(db, ops.getAsset))
	mux.HandleFunc("DELETE /api/ops/assets/{key}", tenantAuth(db, ops.deleteAsset))

	srv := &http.Server{
		Addr:    cfg.Server.ListenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	log.Printf("serve: listening on %s", cfg.Server.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
