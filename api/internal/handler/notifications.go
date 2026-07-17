package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

const defaultNotificationPage = 1

var allowedNotificationTypes = map[string]struct{}{
	"import_complete":     {},
	"import_failed":       {},
	"sync_complete":       {},
	"qsl_confirmed":       {},
	"system_announcement": {},
	"spot_alert":          {},
}

// NotificationHandler handles in-app notification endpoints.
type NotificationHandler struct {
	pool *pgxpool.Pool
}

// NewNotificationHandler creates a NotificationHandler.
func NewNotificationHandler(pool *pgxpool.Pool) *NotificationHandler {
	return &NotificationHandler{pool: pool}
}

type createNotificationRequest struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type notificationResponse struct {
	UUID      string         `json:"uuid"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	IsRead    bool           `json:"is_read"`
	ReadAt    *time.Time     `json:"read_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req createNotificationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	notificationType := strings.TrimSpace(req.Type)
	if _, ok := allowedNotificationTypes[notificationType]; !ok {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid notification type")
		return
	}

	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "payload must be valid JSON object")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	row, err := queries.CreateNotification(r.Context(), db.CreateNotificationParams{
		UserID:  userID,
		Type:    notificationType,
		Payload: payloadJSON,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "notification create failed", "could not create notification")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "notification create failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusCreated, "notification created", notificationFromRow(row))
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	page := parsePage(r.URL.Query().Get("page"))
	pageSize := parsePageSize(r.URL.Query().Get("page_size"))
	offset := int32((page - 1) * int(pageSize))
	unreadOnly := r.URL.Query().Get("unread") == "true"

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	var rows []db.Notification
	if unreadOnly {
		rows, err = queries.ListUnreadNotifications(r.Context(), db.ListUnreadNotificationsParams{
			UserID: userID,
			Limit:  pageSize,
			Offset: offset,
		})
	} else {
		rows, err = queries.ListNotifications(r.Context(), db.ListNotificationsParams{
			UserID: userID,
			Limit:  pageSize,
			Offset: offset,
		})
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not list notifications")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	items := make([]notificationResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationFromRow(row))
	}

	writeSuccess(w, http.StatusOK, "notifications listed", map[string]any{
		"items":     items,
		"page":      page,
		"page_size": pageSize,
		"count":     len(items),
	})
}

func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	notificationUUID, err := uuid.Parse(chi.URLParam(r, "notificationUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid notification UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	affected, err := queries.MarkNotificationRead(r.Context(), db.MarkNotificationReadParams{
		Uuid:   notificationUUID,
		UserID: userID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "could not mark notification as read")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusOK, "notification not found", "notification not found")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "notification marked read", map[string]string{"uuid": notificationUUID.String()})
}

func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
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
	queries := db.New(tx)
	affected, err := queries.MarkAllNotificationsRead(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "could not mark notifications as read")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "update failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "notifications marked read", map[string]int64{"updated": affected})
}

func (h *NotificationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	notificationUUID, err := uuid.Parse(chi.URLParam(r, "notificationUUID"))
	if err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid notification UUID")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to prepare tenant context")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	queries := db.New(tx)
	affected, err := queries.DeleteNotification(r.Context(), db.DeleteNotificationParams{
		Uuid:   notificationUUID,
		UserID: userID,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "delete failed", "could not delete notification")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusOK, "notification not found", "notification not found")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "delete failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "notification deleted", map[string]string{"uuid": notificationUUID.String()})
}

func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
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
	queries := db.New(tx)
	count, err := queries.CountUnreadNotifications(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "could not count unread notifications")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "query failed", "transaction failed")
		return
	}

	writeSuccess(w, http.StatusOK, "unread count retrieved", map[string]int64{"count": count})
}

func notificationFromRow(row db.Notification) notificationResponse {
	payload := map[string]any{}
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			payload = map[string]any{"raw": string(row.Payload)}
		}
	}

	resp := notificationResponse{
		UUID:      row.Uuid.String(),
		Type:      row.Type,
		Payload:   payload,
		IsRead:    row.ReadAt.Valid,
		CreatedAt: row.CreatedAt.Time.UTC(),
	}
	if row.ReadAt.Valid {
		readAt := row.ReadAt.Time.UTC()
		resp.ReadAt = &readAt
	}
	return resp
}

func parsePage(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return defaultNotificationPage
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p <= 0 {
		return defaultNotificationPage
	}
	return p
}
