package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/config"
)

const defaultMaxInvitesPerUser = 5

type inviteKeyResponse struct {
	ID        int64      `json:"id"`
	Code      string     `json:"code"`
	CreatedBy int64      `json:"created_by"`
	UsedBy    *int64     `json:"used_by,omitempty"`
	MaxUses   int32      `json:"max_uses"`
	UsesCount int32      `json:"uses_count"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

func TestInvites_CreateListRevoke(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
	})

	owner := createTestUser(t, pool, "invite-owner")

	createStatus, createEnv := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, map[string]any{"max_uses": 2})
	if createStatus != http.StatusCreated || !createEnv.Success {
		t.Fatalf("create invite failed: status=%d success=%v error=%q", createStatus, createEnv.Success, createEnv.Error)
	}

	var created inviteKeyResponse
	decodeData(t, createEnv.Data, &created)
	if len(created.Code) != 8 {
		t.Fatalf("expected 8-char invite code, got %q", created.Code)
	}
	if created.MaxUses != 2 {
		t.Fatalf("expected max_uses=2, got %d", created.MaxUses)
	}

	listStatus, listEnv := doJSON(t, h, http.MethodGet, "/v1/invites", owner.ID, nil)
	if listStatus != http.StatusOK || !listEnv.Success {
		t.Fatalf("list invites failed: status=%d success=%v error=%q", listStatus, listEnv.Success, listEnv.Error)
	}
	var listed struct {
		Items []inviteKeyResponse `json:"items"`
	}
	decodeData(t, listEnv.Data, &listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("unexpected invite list payload: %+v", listed.Items)
	}

	revokeStatus, revokeEnv := doJSON(t, h, http.MethodDelete, fmt.Sprintf("/v1/invites/%d", created.ID), owner.ID, nil)
	if revokeStatus != http.StatusOK || !revokeEnv.Success {
		t.Fatalf("revoke invite failed: status=%d success=%v error=%q", revokeStatus, revokeEnv.Success, revokeEnv.Error)
	}

	listStatus, listEnv = doJSON(t, h, http.MethodGet, "/v1/invites", owner.ID, nil)
	if listStatus != http.StatusOK || !listEnv.Success {
		t.Fatalf("list invites after revoke failed: status=%d success=%v error=%q", listStatus, listEnv.Success, listEnv.Error)
	}
	decodeData(t, listEnv.Data, &listed)
	if len(listed.Items) != 1 || listed.Items[0].RevokedAt == nil {
		t.Fatalf("expected revoked invite in list, got %+v", listed.Items)
	}
}

func TestInvites_RegistrationWithValidInviteConsumesCode(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
	})

	owner := createTestUser(t, pool, "invite-register-owner")
	invite := createInviteViaAPI(t, h, owner.ID, nil)

	email := fmt.Sprintf("invite_reg_%d@example.test", time.Now().UnixNano())
	status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":       email,
		"password":    "secure-pass-123",
		"invite_code": invite.Code,
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("register with invite failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var reg struct {
		User struct {
			UUID string `json:"uuid"`
		} `json:"user"`
	}
	decodeData(t, env.Data, &reg)

	var userID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE uuid = $1`, reg.User.UUID).Scan(&userID); err != nil {
		t.Fatalf("lookup registered user id: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM logbooks WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	var usesCount int32
	var usedBy *int64
	if err := pool.QueryRow(context.Background(), `SELECT uses_count, used_by FROM invite_keys WHERE id = $1`, invite.ID).Scan(&usesCount, &usedBy); err != nil {
		t.Fatalf("query invite usage: %v", err)
	}
	if usesCount != 1 {
		t.Fatalf("expected uses_count=1, got %d", usesCount)
	}
	if usedBy == nil || *usedBy != userID {
		t.Fatalf("expected used_by=%d, got %v", userID, usedBy)
	}
}

func TestInvites_RegistrationRejectsInvalidExpiredOrRevokedInvite(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
	})

	owner := createTestUser(t, pool, "invite-invalid-owner")
	expired := createInviteViaAPI(t, h, owner.ID, map[string]any{"expires_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339)})
	revoked := createInviteViaAPI(t, h, owner.ID, nil)
	_, _ = doJSON(t, h, http.MethodDelete, fmt.Sprintf("/v1/invites/%d", revoked.ID), owner.ID, nil)

	for name, code := range map[string]string{
		"invalid": "ABCDEFGH",
		"expired": expired.Code,
		"revoked": revoked.Code,
	} {
		t.Run(name, func(t *testing.T) {
			status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
				"email":       fmt.Sprintf("%s_%d@example.test", name, time.Now().UnixNano()),
				"password":    "secure-pass-123",
				"invite_code": code,
			})
			if status != http.StatusForbidden || env.Success {
				t.Fatalf("expected forbidden for %s invite, got status=%d success=%v error=%q", name, status, env.Success, env.Error)
			}
		})
	}
}

func TestInvites_RegistrationRequiresInviteWhenEnabled(t *testing.T) {
	_, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
	})

	status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":    fmt.Sprintf("noinvite_%d@example.test", time.Now().UnixNano()),
		"password": "secure-pass-123",
	})
	if status != http.StatusForbidden || env.Success {
		t.Fatalf("expected invite-required rejection, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func TestInvites_RegistrationAllowsNoInviteWhenDisabled(t *testing.T) {
	pool, h := setupIntegration(t)

	email := fmt.Sprintf("openreg_%d@example.test", time.Now().UnixNano())
	status, env := doJSON(t, h, http.MethodPost, "/v1/auth/register", 0, map[string]any{
		"email":    email,
		"password": "secure-pass-123",
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("expected open registration success, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var reg struct {
		User struct {
			UUID string `json:"uuid"`
		} `json:"user"`
	}
	decodeData(t, env.Data, &reg)

	var userID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE uuid = $1`, reg.User.UUID).Scan(&userID); err != nil {
		t.Fatalf("lookup user id: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM logbooks WHERE user_id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
}

func TestInvites_RateLimitMaximumFiveActiveInvites(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
	})
	owner := createTestUser(t, pool, "invite-rate-owner")

	for i := 0; i < defaultMaxInvitesPerUser; i++ {
		status, env := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, nil)
		if status != http.StatusCreated || !env.Success {
			t.Fatalf("create invite %d failed: status=%d success=%v error=%q", i+1, status, env.Success, env.Error)
		}
	}

	status, env := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, nil)
	if status != http.StatusTooManyRequests || env.Success {
		t.Fatalf("expected rate limit on sixth invite, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func TestInvites_RateLimitUsesConfiguredMaximum(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
		cfg.MaxInvitesPerUser = 2
	})
	owner := createTestUser(t, pool, "invite-configured-max-owner")

	for i := 0; i < 2; i++ {
		status, env := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, nil)
		if status != http.StatusCreated || !env.Success {
			t.Fatalf("create invite %d failed: status=%d success=%v error=%q", i+1, status, env.Success, env.Error)
		}
	}

	status, env := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, nil)
	if status != http.StatusTooManyRequests || env.Success {
		t.Fatalf("expected rate limit at configured max, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func TestInvites_ReplenishAllowsOldConsumedInviteToFallOff(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
		cfg.MaxInvitesPerUser = 1
		cfg.InviteReplenishEnabled = true
		cfg.InviteReplenishInterval = time.Hour
	})
	owner := createTestUser(t, pool, "invite-replenish-old-owner")

	seedInviteKey(t, pool, owner.ID, "ABCDEFG2", 1, 1, time.Now().Add(-2*time.Hour))

	status, env := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, nil)
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("expected old consumed invite to fall off, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func TestInvites_ReplenishCountsRecentlyConsumedInvite(t *testing.T) {
	pool, h := setupIntegrationWithConfig(t, func(cfg *config.Config) {
		cfg.RequireInviteKey = true
		cfg.MaxInvitesPerUser = 1
		cfg.InviteReplenishEnabled = true
		cfg.InviteReplenishInterval = time.Hour
	})
	owner := createTestUser(t, pool, "invite-replenish-recent-owner")

	seedInviteKey(t, pool, owner.ID, "ABCDEFG3", 1, 1, time.Now().Add(-30*time.Minute))

	status, env := doJSON(t, h, http.MethodPost, "/v1/invites", owner.ID, nil)
	if status != http.StatusTooManyRequests || env.Success {
		t.Fatalf("expected recent consumed invite to count toward limit, got status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func createInviteViaAPI(t *testing.T, h http.Handler, ownerID int64, body map[string]any) inviteKeyResponse {
	t.Helper()
	status, env := doJSON(t, h, http.MethodPost, "/v1/invites", ownerID, body)
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create invite via api failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var invite inviteKeyResponse
	decodeData(t, env.Data, &invite)
	return invite
}

func seedInviteKey(t *testing.T, pool *pgxpool.Pool, ownerID int64, code string, maxUses, usesCount int32, createdAt time.Time) {
	t.Helper()

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO invite_keys (code, created_by, used_by, max_uses, uses_count, created_at)
		VALUES ($1, $2, $2, $3, $4, $5)
	`, code, ownerID, maxUses, usesCount, createdAt.UTC()); err != nil {
		t.Fatalf("seed invite key: %v", err)
	}
}
