package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	"github.com/FtlC-ian/radioledger/api/internal/database"
	syncsvc "github.com/FtlC-ian/radioledger/api/internal/services/sync"
)

func main() {
	if err := run(); err != nil {
		slog.Error("credential key rotation failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	batchSize := flag.Int("batch-size", 100, "number of credential rows to rotate per batch")
	flag.Parse()

	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.MasterKey == "" {
		return fmt.Errorf("RADIOLEDGER_MASTER_KEY must be set to the current key")
	}

	newKeyB64 := os.Getenv("RADIOLEDGER_NEW_MASTER_KEY")
	if newKeyB64 == "" {
		return fmt.Errorf("RADIOLEDGER_NEW_MASTER_KEY must be set")
	}

	current, err := crypto.NewKeyringFromBase64(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("invalid RADIOLEDGER_MASTER_KEY: %w", err)
	}

	newRaw, err := base64.StdEncoding.DecodeString(newKeyB64)
	if err != nil {
		newRaw, err = base64.RawURLEncoding.DecodeString(newKeyB64)
		if err != nil {
			return fmt.Errorf("invalid RADIOLEDGER_NEW_MASTER_KEY (base64): %w", err)
		}
	}
	if err := current.AddKey(current.CurrentVersion()+1, newRaw); err != nil {
		return fmt.Errorf("add new key version: %w", err)
	}

	pool, err := database.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	store := syncsvc.NewPostgresStore(pool, current)
	started := time.Now()
	rotated, err := store.RotateKey(ctx, *batchSize)
	if err != nil {
		return fmt.Errorf("rotate credentials: %w", err)
	}

	slog.Info("credential key rotation completed",
		slog.Int("rotated_rows", rotated),
		slog.Int("new_key_version", int(current.CurrentVersion())),
		slog.Duration("duration", time.Since(started)),
	)
	slog.Warn("Update RADIOLEDGER_MASTER_KEY to RADIOLEDGER_NEW_MASTER_KEY before next server restart.")
	return nil
}
