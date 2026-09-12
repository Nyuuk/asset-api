package main

import (
	"context"
	"log"
	"time"
)

const expiredBatchSize = 100

func runWorker(ctx context.Context, cfg *Config, db *DB, s3 *S3Client) {
	interval := time.Duration(cfg.TTL.CleanupIntervalSeconds) * time.Second
	log.Printf("worker: cleanup loop starting, interval=%s", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	cleanupOnce(ctx, db, s3)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupOnce(ctx, db, s3)
		}
	}
}

func cleanupOnce(ctx context.Context, db *DB, s3 *S3Client) {
	expired, err := db.ListExpiredAssets(ctx, expiredBatchSize)
	if err != nil {
		log.Printf("worker: list expired assets failed: %v", err)
		return
	}
	if len(expired) == 0 {
		return
	}

	log.Printf("worker: cleaning up %d expired asset(s)", len(expired))
	for _, a := range expired {
		if err := s3.Delete(ctx, a.Key); err != nil {
			log.Printf("worker: delete %s from s3 failed: %v", a.Key, err)
			continue
		}
		if err := db.MarkAssetDeleted(ctx, a.ID); err != nil {
			log.Printf("worker: mark %s deleted failed: %v", a.Key, err)
		}
	}
}
