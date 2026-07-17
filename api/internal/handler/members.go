package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

// MemberHandler handles logbook membership and ownership transfer endpoints.
type MemberHandler struct {
	pool *pgxpool.Pool
}

// NewMemberHandler constructs a MemberHandler.
func NewMemberHandler(pool *pgxpool.Pool) *MemberHandler {
	return &MemberHandler{pool: pool}
}

type memberInviteRequest struct {
	Email    *string `json:"email"`
	Callsign *string `json:"callsign"`
	Role     string  `json:"role"`
}

type memberRoleUpdateRequest struct {
	Role string `json:"role"`
}

type transferOwnershipRequest struct {
	UserUUID string `json:"user_uuid"`
}

type memberResponse struct {
	UserUUID    string     `json:"user_uuid"`
	Email       string     `json:"email"`
	Callsign    *string    `json:"callsign,omitempty"`
	DisplayName *string    `json:"display_name,omitempty"`
	Role        string     `json:"role"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	InvitedBy   *string    `json:"invited_by,omitempty"`
	RemovedAt   *time.Time `json:"removed_at,omitempty"`
}

// Invite handles POST /v1/logbooks/{logbookUUID}/members.
func (h *MemberHandler) Invite(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := logbookPathID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req memberInviteRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	role, err := parseMemberRole(req.Role, false)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	targetUser, err := resolveMemberTargetUser(r.Context(), queries, req.Email, req.Callsign)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	existing, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUser.Uuid,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusInternalServerError, "failed to invite member", "query failed")
		return
	}
	if err == nil && existing.Role == string(auth.RoleOwner) && role != auth.RoleOwner {
		writeFailure(w, http.StatusBadRequest, "invalid request", "cannot change owner role via members endpoint; use transfer ownership")
		return
	}

	invitedBy := &userID
	_, err = queries.UpsertLogbookMemberRole(r.Context(), db.UpsertLogbookMemberRoleParams{
		LogbookUuid: logbookUUID,
		UserID:      targetUser.ID,
		Role:        string(role),
		InvitedBy:   invitedBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to invite member", "update failed")
		return
	}

	member, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUser.Uuid,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to invite member", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to invite member", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "member invited", memberFromGetRow(member, invitedBy))
}

// List handles GET /v1/logbooks/{logbookUUID}/members.
func (h *MemberHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := logbookPathID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	rows, err := queries.ListLogbookMembers(r.Context(), logbookUUID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list members", "query failed")
		return
	}

	items := make([]memberResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, memberFromListRow(row, nil))
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to list members", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "members listed", map[string]any{"items": items})
}

// UpdateRole handles PUT /v1/logbooks/{logbookUUID}/members/{userUUID}.
func (h *MemberHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := logbookPathID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	targetUserUUID, err := uuid.Parse(chi.URLParam(r, "userUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid user UUID")
		return
	}

	var req memberRoleUpdateRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	role, err := parseMemberRole(req.Role, false)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	existing, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "member not found", "member not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update member", "query failed")
		return
	}
	if existing.Role == string(auth.RoleOwner) {
		writeFailure(w, http.StatusBadRequest, "invalid request", "cannot change owner role via members endpoint; use transfer ownership")
		return
	}

	invitedBy := &userID
	_, err = queries.UpdateLogbookMemberRoleByUserUUID(r.Context(), db.UpdateLogbookMemberRoleByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
		Role:        string(role),
		InvitedBy:   invitedBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "member not found", "member not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update member", "update failed")
		return
	}

	member, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update member", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to update member", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "member role updated", memberFromGetRow(member, invitedBy))
}

// Remove handles DELETE /v1/logbooks/{logbookUUID}/members/{userUUID}.
func (h *MemberHandler) Remove(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := logbookPathID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	targetUserUUID, err := uuid.Parse(chi.URLParam(r, "userUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid user UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	existing, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "member not found", "member not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to remove member", "query failed")
		return
	}
	if existing.Role == string(auth.RoleOwner) {
		writeFailure(w, http.StatusBadRequest, "invalid request", "owner cannot be removed; transfer ownership first")
		return
	}

	affected, err := queries.DeleteLogbookMemberByUserUUID(r.Context(), db.DeleteLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to remove member", "delete failed")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusOK, "member not found", "member not found")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to remove member", "transaction failed")
		return
	}

	now := time.Now().UTC()
	payload := memberFromGetRow(existing, nil)
	payload.RemovedAt = &now
	writeSuccess(w, http.StatusOK, "member removed", payload)
}

// TransferOwnership handles POST /v1/logbooks/{logbookUUID}/transfer-ownership.
func (h *MemberHandler) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	logbookUUID, err := logbookPathID(r)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	var req transferOwnershipRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.UserUUID) == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "user_uuid is required")
		return
	}

	targetUserUUID, err := uuid.Parse(req.UserUUID)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid user_uuid")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	owner, err := queries.GetLogbookOwner(r.Context(), logbookUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "query failed")
		return
	}
	if owner.UserID != userID {
		writeFailure(w, http.StatusForbidden, "forbidden", "only the owner can transfer ownership")
		return
	}

	targetMember, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusBadRequest, "invalid request", "target user must already be a logbook member")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "query failed")
		return
	}

	if targetMember.Role == string(auth.RoleOwner) {
		writeSuccess(w, http.StatusOK, "ownership transferred", memberFromGetRow(targetMember, nil))
		return
	}

	invitedBy := &userID

	demoted, err := queries.SetLogbookMemberRoleByUserID(r.Context(), db.SetLogbookMemberRoleByUserIDParams{
		LogbookID: targetMember.LogbookID,
		UserID:    owner.UserID,
		Role:      string(auth.RoleAdmin),
		InvitedBy: invitedBy,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "failed to demote previous owner")
		return
	}
	if demoted == 0 {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "owner membership missing")
		return
	}

	promoted, err := queries.SetLogbookMemberRoleByUserID(r.Context(), db.SetLogbookMemberRoleByUserIDParams{
		LogbookID: targetMember.LogbookID,
		UserID:    targetMember.UserID,
		Role:      string(auth.RoleOwner),
		InvitedBy: invitedBy,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "failed to promote new owner")
		return
	}
	if promoted == 0 {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "target membership missing")
		return
	}

	updated, err := queries.SetLogbookOwnerUser(r.Context(), db.SetLogbookOwnerUserParams{
		LogbookUuid: logbookUUID,
		UserID:      targetMember.UserID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "failed to update logbook owner")
		return
	}
	if updated == 0 {
		writeFailure(w, http.StatusOK, "logbook not found", "logbook not found")
		return
	}

	member, err := queries.GetLogbookMemberByUserUUID(r.Context(), db.GetLogbookMemberByUserUUIDParams{
		LogbookUuid: logbookUUID,
		UserUuid:    targetUserUUID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to transfer ownership", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "ownership transferred", memberFromGetRow(member, invitedBy))
}

func parseMemberRole(raw string, allowOwner bool) (auth.Role, error) {
	role, ok := auth.ParseRole(raw)
	if !ok {
		return "", fmt.Errorf("invalid role")
	}
	if !allowOwner && role == auth.RoleOwner {
		return "", fmt.Errorf("owner role can only be assigned via transfer ownership")
	}
	return role, nil
}

func resolveMemberTargetUser(ctx context.Context, queries *db.Queries, email, callsign *string) (db.GetUserByEmailRow, error) {
	emailVal := normalizeOptional(email)
	callsignVal := normalizeOptional(callsign)

	if emailVal == nil && callsignVal == nil {
		return db.GetUserByEmailRow{}, fmt.Errorf("provide email or callsign")
	}
	if emailVal != nil && callsignVal != nil {
		return db.GetUserByEmailRow{}, fmt.Errorf("provide either email or callsign, not both")
	}

	if emailVal != nil {
		row, err := queries.GetUserByEmail(ctx, *emailVal)
		if errors.Is(err, pgx.ErrNoRows) {
			return db.GetUserByEmailRow{}, fmt.Errorf("user not found")
		}
		if err != nil {
			return db.GetUserByEmailRow{}, fmt.Errorf("failed to resolve user")
		}
		return row, nil
	}

	row, err := queries.GetUserByCallsign(ctx, *callsignVal)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetUserByEmailRow{}, fmt.Errorf("user not found")
	}
	if err != nil {
		return db.GetUserByEmailRow{}, fmt.Errorf("failed to resolve user")
	}
	return db.GetUserByEmailRow(row), nil
}

func memberFromListRow(row db.ListLogbookMembersRow, invitedBy *int64) memberResponse {
	return memberResponse{
		UserUUID:    row.UserUuid.String(),
		Email:       row.Email,
		Callsign:    row.Callsign,
		DisplayName: row.DisplayName,
		Role:        row.Role,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
		InvitedBy:   toUUIDString(invitedBy),
	}
}

func memberFromGetRow(row db.GetLogbookMemberByUserUUIDRow, invitedBy *int64) memberResponse {
	return memberResponse{
		UserUUID:    row.UserUuid.String(),
		Email:       row.Email,
		Callsign:    row.Callsign,
		DisplayName: row.DisplayName,
		Role:        row.Role,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
		InvitedBy:   toUUIDString(invitedBy),
	}
}

func toUUIDString(userID *int64) *string {
	if userID == nil {
		return nil
	}
	v := fmt.Sprintf("%d", *userID)
	return &v
}
