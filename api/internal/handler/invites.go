package handler

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/config"
	"github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

const (
	inviteCodeLength = 8
	inviteAlphabet   = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
)

type InviteHandler struct {
	pool *pgxpool.Pool
	cfg  *config.Config
}

type createInviteRequest struct {
	MaxUses   *int32     `json:"max_uses"`
	ExpiresAt *time.Time `json:"expires_at"`
}

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

func NewInviteHandler(pool *pgxpool.Pool, cfg *config.Config) *InviteHandler {
	return &InviteHandler{pool: pool, cfg: cfg}
}

func (h *InviteHandler) maxInvitesPerUser() int {
	if h == nil {
		return 5
	}
	return h.cfg.EffectiveMaxInvitesPerUser()
}

func (h *InviteHandler) inviteReplenishEnabled() bool {
	return h != nil && h.cfg != nil && h.cfg.InviteReplenishEnabled
}

func (h *InviteHandler) inviteReplenishInterval() time.Duration {
	if h == nil {
		return 168 * time.Hour
	}
	return h.cfg.EffectiveInviteReplenishInterval()
}

func (h *InviteHandler) countInvitesForLimit(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, userID int64) (int64, error) {
	if !h.inviteReplenishEnabled() {
		return q.CountActiveInvitesByCreator(ctx, userID)
	}

	cutoff := time.Now().Add(-h.inviteReplenishInterval())
	var count int64
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM invite_keys
		WHERE created_by = $1
		  AND revoked_at IS NULL
		  AND (
				(uses_count < max_uses AND (expires_at IS NULL OR expires_at > NOW()))
				OR (uses_count >= max_uses AND created_at > $2)
		  )
	`, userID, cutoff).Scan(&count)
	return count, err
}

func (h *InviteHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req createInviteRequest
	if err := decodeOptionalJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if req.MaxUses != nil && *req.MaxUses < 1 {
		writeFailure(w, http.StatusBadRequest, "invalid request", "max_uses must be at least 1")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	q := sqlc.New(tx)
	activeCount, err := h.countInvitesForLimit(r.Context(), tx, q, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create invite", "rate limit query failed")
		return
	}
	maxInvites := h.maxInvitesPerUser()
	if activeCount >= int64(maxInvites) {
		writeFailure(w, http.StatusTooManyRequests, "too many active invites", fmt.Sprintf("maximum %d active invites allowed", maxInvites))
		return
	}

	arg := sqlc.CreateInviteKeyParams{
		CreatedBy: userID,
		MaxUses:   req.MaxUses,
		ExpiresAt: timestamptzFromPtr(req.ExpiresAt),
	}

	var invite sqlc.InviteKey
	for attempt := 0; attempt < 8; attempt++ {
		code, genErr := generateInviteCode()
		if genErr != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to create invite", "failed to generate invite code")
			return
		}
		arg.Code = code

		invite, err = q.CreateInviteKey(r.Context(), arg)
		if err == nil {
			break
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue
		}
		writeFailure(w, http.StatusInternalServerError, "failed to create invite", "insert failed")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create invite", "unable to allocate unique invite code")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to create invite", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "invite created", inviteKeyResponseFromModel(invite))
}

func (h *InviteHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	q := sqlc.New(tx)
	invites, err := q.ListInviteKeysByCreator(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list invites", "query failed")
		return
	}

	items := make([]inviteKeyResponse, 0, len(invites))
	for _, invite := range invites {
		items = append(items, inviteKeyResponseFromModel(invite))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list invites", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "invites listed", map[string]any{"items": items})
}

func (h *InviteHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	inviteID, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || inviteID <= 0 {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid invite id")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	q := sqlc.New(tx)
	affected, err := q.RevokeInviteKey(r.Context(), sqlc.RevokeInviteKeyParams{ID: inviteID, CreatedBy: userID})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to revoke invite", "update failed")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusNotFound, "invite not found", "invite not found")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to revoke invite", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "invite revoked", nil)
}

func inviteKeyResponseFromModel(invite sqlc.InviteKey) inviteKeyResponse {
	return inviteKeyResponse{
		ID:        invite.ID,
		Code:      invite.Code,
		CreatedBy: invite.CreatedBy,
		UsedBy:    invite.UsedBy,
		MaxUses:   invite.MaxUses,
		UsesCount: invite.UsesCount,
		ExpiresAt: timePtrFromTimestamptz(invite.ExpiresAt),
		CreatedAt: invite.CreatedAt.Time.UTC(),
		RevokedAt: timePtrFromTimestamptz(invite.RevokedAt),
	}
}

func generateInviteCode() (string, error) {
	var b strings.Builder
	b.Grow(inviteCodeLength)
	for b.Len() < inviteCodeLength {
		var buf [1]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		idx := int(buf[0])
		if idx >= len(inviteAlphabet)*(256/len(inviteAlphabet)) {
			continue
		}
		b.WriteByte(inviteAlphabet[idx%len(inviteAlphabet)])
	}
	return b.String(), nil
}

func normalizeInviteCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func isValidInviteCodeFormat(code string) bool {
	if len(code) != inviteCodeLength {
		return false
	}
	for _, ch := range code {
		if !strings.ContainsRune(inviteAlphabet, ch) {
			return false
		}
	}
	return true
}

func timePtrFromTimestamptz(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time.UTC()
	return &t
}

func timestamptzFromPtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func decodeOptionalJSONBody(r *http.Request, dst any) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	if err := decodeJSONBody(r, dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
