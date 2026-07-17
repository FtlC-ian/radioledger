package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/FtlC-ian/radioledger/api/internal/jobs"
)

type notificationItem struct {
	UUID      string         `json:"uuid"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	IsRead    bool           `json:"is_read"`
	ReadAt    *time.Time     `json:"read_at"`
	CreatedAt time.Time      `json:"created_at"`
}

type notificationListPayload struct {
	Items    []notificationItem `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Count    int                `json:"count"`
}

type unreadCountPayload struct {
	Count int64 `json:"count"`
}

func TestIntegration_ImportSSEProgressStream(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "sse")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "SSE Logbook", true)

	var logbookID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID); err != nil {
		t.Fatalf("get logbook id: %v", err)
	}

	var importID int64
	var importUUID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO import_jobs (user_id, logbook_id, filename, status, total_records, imported, duplicate, errors, source, dedup_strategy, timestamp_strategy, started_at)
		VALUES ($1, $2, 'sse.adi', 'processing', 10, 0, 0, 0, 'web', 'skip', 'trust_utc', NOW())
		RETURNING id, uuid
	`, user.ID, logbookID).Scan(&importID, &importUUID)
	if err != nil {
		t.Fatalf("insert import job: %v", err)
	}

	go func() {
		time.Sleep(700 * time.Millisecond)
		_, _ = pool.Exec(context.Background(), `UPDATE import_jobs SET imported = 4, duplicate = 1, errors = 0 WHERE id = $1`, importID)
		time.Sleep(700 * time.Millisecond)
		_, _ = pool.Exec(context.Background(), `
			UPDATE import_jobs
			SET status = 'complete', imported = 8, duplicate = 2, errors = 0, completed_at = NOW()
			WHERE id = $1
		`, importID)
	}()

	req := httptest.NewRequest(http.MethodGet, "/v1/import/"+importUUID.String()+"/stream", nil)
	setTestAuthHeader(t, req, user.ID)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("timed out waiting for SSE stream to finish")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream content-type, got %q", got)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: progress") {
		t.Fatalf("expected progress event in SSE body, got:\n%s", body)
	}
	if !strings.Contains(body, "event: complete") {
		t.Fatalf("expected complete event in SSE body, got:\n%s", body)
	}
	if !strings.Contains(body, `"status":"completed"`) {
		t.Fatalf("expected completed payload in SSE body, got:\n%s", body)
	}
}

func TestIntegration_ImportSSEStreamTokenFlow(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "sse-stream-token")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "SSE Stream Token Logbook", true)

	var logbookID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID); err != nil {
		t.Fatalf("get logbook id: %v", err)
	}

	var importUUID uuid.UUID
	err := pool.QueryRow(context.Background(), `
		INSERT INTO import_jobs (user_id, logbook_id, filename, status, total_records, imported, duplicate, errors, source, dedup_strategy, timestamp_strategy, started_at, completed_at)
		VALUES ($1, $2, 'sse-stream-token.adi', 'complete', 5, 4, 1, 0, 'web', 'skip', 'trust_utc', NOW(), NOW())
		RETURNING uuid
	`, user.ID, logbookID).Scan(&importUUID)
	if err != nil {
		t.Fatalf("insert import job: %v", err)
	}

	status, env := doJSON(t, h, http.MethodPost, "/v1/stream-token", user.ID, map[string]any{
		"path": "/v1/import/" + importUUID.String() + "/stream",
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("stream token endpoint failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	decodeData(t, env.Data, &tokenResp)
	if tokenResp.Token == "" {
		t.Fatal("expected non-empty stream token")
	}

	streamReq := httptest.NewRequest(http.MethodGet, "/v1/import/"+importUUID.String()+"/stream?stream_token="+tokenResp.Token, nil)
	streamRec := httptest.NewRecorder()
	h.ServeHTTP(streamRec, streamReq)
	if streamRec.Code != http.StatusOK {
		t.Fatalf("expected SSE stream with token to return 200, got %d", streamRec.Code)
	}
	if !strings.Contains(streamRec.Body.String(), "event: complete") {
		t.Fatalf("expected complete event in SSE body, got:\n%s", streamRec.Body.String())
	}

	reuseReq := httptest.NewRequest(http.MethodGet, "/v1/import/"+importUUID.String()+"/stream?stream_token="+tokenResp.Token, nil)
	reuseRec := httptest.NewRecorder()
	h.ServeHTTP(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected reused token to be rejected with 401, got %d", reuseRec.Code)
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/v1/import/"+importUUID.String()+"/stream?access_token=dev-user-"+fmt.Sprint(user.ID), nil)
	legacyRec := httptest.NewRecorder()
	h.ServeHTTP(legacyRec, legacyReq)
	if legacyRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected access_token query auth to be rejected with 401, got %d", legacyRec.Code)
	}
}

func TestIntegration_NotificationsCRUDAndRLS(t *testing.T) {
	pool, h := setupIntegration(t)
	userA := createTestUser(t, pool, "notif-a")
	userB := createTestUser(t, pool, "notif-b")

	status, env := doJSON(t, h, http.MethodPost, "/v1/notifications", userA.ID, map[string]any{
		"type": "system_announcement",
		"payload": map[string]any{
			"title":   "Maintenance",
			"message": "Station sync maintenance tonight",
			"route":   "/settings",
		},
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create notification failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var created notificationItem
	decodeData(t, env.Data, &created)
	if created.UUID == "" {
		t.Fatal("expected created notification uuid")
	}
	if created.IsRead {
		t.Fatal("new notification should be unread")
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/notifications/unread-count", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("unread-count A failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var unreadA unreadCountPayload
	decodeData(t, env.Data, &unreadA)
	if unreadA.Count != 1 {
		t.Fatalf("expected unread count 1 for user A, got %d", unreadA.Count)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/notifications/unread-count", userB.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("unread-count B failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var unreadB unreadCountPayload
	decodeData(t, env.Data, &unreadB)
	if unreadB.Count != 0 {
		t.Fatalf("expected unread count 0 for user B, got %d", unreadB.Count)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/notifications?page=1&page_size=10", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list notifications A failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var listedA notificationListPayload
	decodeData(t, env.Data, &listedA)
	if len(listedA.Items) != 1 {
		t.Fatalf("expected 1 notification for user A, got %d", len(listedA.Items))
	}
	if listedA.Items[0].UUID != created.UUID {
		t.Fatalf("expected notification uuid %s, got %s", created.UUID, listedA.Items[0].UUID)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/notifications?page=1&page_size=10", userB.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list notifications B failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	var listedB notificationListPayload
	decodeData(t, env.Data, &listedB)
	if len(listedB.Items) != 0 {
		t.Fatalf("expected 0 notifications for user B, got %d", len(listedB.Items))
	}

	status, env = doJSON(t, h, http.MethodPut, "/v1/notifications/"+created.UUID+"/read", userB.ID, nil)
	if status != http.StatusOK || env.Success {
		t.Fatalf("user B should not mark user A notification as read: status=%d success=%v", status, env.Success)
	}

	status, env = doJSON(t, h, http.MethodPut, "/v1/notifications/"+created.UUID+"/read", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("mark read failed for user A: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/notifications/unread-count", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("unread-count after mark read failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
	decodeData(t, env.Data, &unreadA)
	if unreadA.Count != 0 {
		t.Fatalf("expected unread count 0 after mark read, got %d", unreadA.Count)
	}

	status, env = doJSON(t, h, http.MethodPost, "/v1/notifications", userA.ID, map[string]any{
		"type":    "import_complete",
		"payload": map[string]any{"message": "Imported 10 QSOs"},
	})
	if status != http.StatusCreated || !env.Success {
		t.Fatalf("create second notification failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodPut, "/v1/notifications/read-all", userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("mark all read failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodDelete, "/v1/notifications/"+created.UUID, userA.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("delete notification failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}
}

func TestIntegration_ADIFImportCreatesNotification(t *testing.T) {
	pool, h := setupIntegration(t)
	user := createTestUser(t, pool, "import-notify")
	logbookUUID := createLogbookViaAPI(t, h, user.ID, "Import Notify", true)

	var logbookID int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM logbooks WHERE uuid = $1`, logbookUUID).Scan(&logbookID); err != nil {
		t.Fatalf("get logbook id: %v", err)
	}

	adifFile, err := os.CreateTemp("", "notify-import-*.adi")
	if err != nil {
		t.Fatalf("create temp adif: %v", err)
	}
	adifPath := adifFile.Name()
	t.Cleanup(func() {
		_ = os.Remove(adifPath)
	})

	adifContent := strings.Join([]string{
		"<ADIF_VER:5>3.1.0",
		"<PROGRAMID:11>RadioLedger",
		"<EOH>",
		"<CALL:4>W1AW<BAND:3>20m<MODE:2>CW<QSO_DATE:8>20260228<TIME_ON:6>120000<EOR>",
	}, "\n")
	if _, err := adifFile.WriteString(adifContent); err != nil {
		_ = adifFile.Close()
		t.Fatalf("write adif content: %v", err)
	}
	if err := adifFile.Close(); err != nil {
		t.Fatalf("close adif file: %v", err)
	}

	var importJobID int64
	var importJobUUID uuid.UUID
	err = pool.QueryRow(context.Background(), `
		INSERT INTO import_jobs (user_id, logbook_id, filename, status, source, dedup_strategy, timestamp_strategy)
		VALUES ($1, $2, 'notify-import.adi', 'pending', 'web', 'skip', 'trust_utc')
		RETURNING id, uuid
	`, user.ID, logbookID).Scan(&importJobID, &importJobUUID)
	if err != nil {
		t.Fatalf("insert import job: %v", err)
	}

	worker := &jobs.ADIFImportWorker{Pool: pool}
	riverJob := &river.Job[jobs.ADIFImportArgs]{
		Args: jobs.ADIFImportArgs{
			ImportJobID: importJobID,
			FilePath:    adifPath,
			LogbookID:   logbookID,
			UserID:      user.ID,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := worker.Work(ctx, riverJob); err != nil {
		t.Fatalf("worker import failed: %v", err)
	}

	status, env := doJSON(t, h, http.MethodGet, "/v1/notifications?page=1&page_size=10", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("list notifications after import failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var listed notificationListPayload
	decodeData(t, env.Data, &listed)
	if len(listed.Items) == 0 {
		t.Fatal("expected at least one notification after import completion")
	}

	found := false
	for _, item := range listed.Items {
		if item.Type != "import_complete" {
			continue
		}
		if got := fmt.Sprint(item.Payload["import_job_uuid"]); got == importJobUUID.String() {
			found = true
			msg := fmt.Sprint(item.Payload["message"])
			if !strings.Contains(msg, "Imported") {
				t.Fatalf("unexpected import notification message: %q", msg)
			}
			break
		}
	}

	if !found {
		raw, _ := json.MarshalIndent(listed.Items, "", "  ")
		t.Fatalf("expected import_complete notification for job %s; got %s", importJobUUID.String(), string(raw))
	}
}
