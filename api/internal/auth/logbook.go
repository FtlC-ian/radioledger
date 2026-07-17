package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateDefaultLogbookForUser provisions the standard default logbook for a new user.
func CreateDefaultLogbookForUser(ctx context.Context, tx pgx.Tx, userID int64) error {
	if _, err := tx.Exec(ctx, `INSERT INTO logbooks (user_id, name, is_default, logbook_type, dedup_window_seconds)
		 VALUES ($1, 'My Log', TRUE, 'general', 60)`, userID); err != nil {
		return fmt.Errorf("create default logbook: %w", err)
	}
	return nil
}
