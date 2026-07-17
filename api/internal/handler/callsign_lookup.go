// Package handler — callsign lookup and cache endpoints.
//
// This file implements:
//   - GET /v1/lookup/{callsign}       — Lookup callsign via cache-first strategy
//   - GET /v1/callsigns/autocomplete  — Prefix search for type-ahead UI
//
// Cache strategy (GET /v1/lookup/{callsign}):
//  1. Check callsign_cache. Cache hit (unexpired) → return immediately, no API call.
//  2. Cache miss + no QRZ credentials → return 200 with success:false.
//  3. Cache miss + QRZ credentials → call QRZ XML API, cache for 30 days, return.
//
// Rate limiting to QRZ: the QRZ client enforces max 1 req/sec. Per-user clients are
// cached in a sync.Map keyed by user ID so QRZ sessions persist across requests.
// Entries are evicted after qrzClientTTL (30 min) of inactivity and immediately on
// credential errors to prevent stale sessions.
//
// callsign_cache is NOT tenant-scoped (no user_id, no RLS). All users share cached
// public callbook data. User credentials still require a tenant context for RLS.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/crypto"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/services/qrz"
)

// qrzClientTTL is the maximum idle lifetime for a cached QRZ client.
// After this duration the entry is evicted and re-created on next use,
// which re-authenticates to QRZ and picks up any credential changes.
const qrzClientTTL = 30 * time.Minute

// qrzClientEntry wraps a QRZ client with a last-used timestamp for TTL eviction.
type qrzClientEntry struct {
	client    *qrz.Client
	lastUsed  time.Time
	mu        sync.Mutex
}

// CallsignLookupHandler handles callsign lookup and autocomplete endpoints.
type CallsignLookupHandler struct {
	pool    *pgxpool.Pool
	keyring *crypto.Keyring

	// qrzClients caches per-user QRZ clients (keyed by int64 user ID).
	// Reusing clients preserves the QRZ session key across requests,
	// avoiding a login round-trip (and rate-limit hit) on every cache miss.
	// Entries have a TTL of qrzClientTTL and are deleted on credential errors.
	qrzClients sync.Map // map[int64]*qrzClientEntry
}

// NewCallsignLookupHandler creates a CallsignLookupHandler.
// keyring may be nil; in that case credential decryption is unavailable and
// the handler operates in cache-only mode.
func NewCallsignLookupHandler(pool *pgxpool.Pool, keyring *crypto.Keyring) *CallsignLookupHandler {
	return &CallsignLookupHandler{pool: pool, keyring: keyring}
}

// callsignInfoResponse is the JSON response shape for a callsign lookup.
type callsignInfoResponse struct {
	Callsign  string    `json:"callsign"`
	FullName  string    `json:"full_name,omitempty"`
	FName     string    `json:"fname,omitempty"`
	LName     string    `json:"lname,omitempty"`
	Addr1     string    `json:"addr1,omitempty"`
	Addr2     string    `json:"addr2,omitempty"`
	State     string    `json:"state,omitempty"`
	Zip       string    `json:"zip,omitempty"`
	Country   string    `json:"country,omitempty"`
	Grid      string    `json:"grid,omitempty"`
	Lat       float64   `json:"lat,omitempty"`
	Lon       float64   `json:"lon,omitempty"`
	Class     string    `json:"class,omitempty"`
	Expires   string    `json:"expires,omitempty"`
	DXCC      int       `json:"dxcc,omitempty"`
	Land      string    `json:"land,omitempty"`
	CQZone    int       `json:"cq_zone,omitempty"`
	ITUZone   int       `json:"itu_zone,omitempty"`
	QSLMgr    string    `json:"qsl_mgr,omitempty"`
	Email     string    `json:"email,omitempty"`
	Image     string    `json:"image,omitempty"`
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
}

// autocompleteItem is one entry in an autocomplete response.
type autocompleteItem struct {
	Callsign string `json:"callsign"`
	FullName string `json:"full_name,omitempty"`
	Grid     string `json:"grid,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/lookup/{callsign}
// ─────────────────────────────────────────────────────────────────────────────

// Lookup handles GET /v1/lookup/{callsign}.
//
// Cache-first strategy:
//  1. Check callsign_cache. Cache hit → return instantly.
//  2. Cache miss + no QRZ credentials → return 200 success:false.
//  3. Cache miss + QRZ credentials → call QRZ, cache 30 days, return.
func (h *CallsignLookupHandler) Lookup(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	callsign := strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "callsign")))
	if callsign == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	ctx := r.Context()

	// ── Step 1: Cache check (callsign_cache has no RLS — plain conn) ──────────
	if cacheRow, cacheErr := h.getCacheEntry(ctx, callsign); cacheErr == nil {
		var info qrz.CallsignInfo
		if jsonErr := json.Unmarshal(cacheRow.Data, &info); jsonErr == nil {
			writeSuccess(w, http.StatusOK, "callsign found (cached)",
				callsignInfoFromQRZ(&info, cacheRow.Source, cacheRow.FetchedAt.Time))
			return
		}
		// Corrupt JSON in cache — log and fall through to re-fetch.
		slog.WarnContext(ctx, "callsign_cache: corrupt JSON, re-fetching",
			slog.String("callsign", callsign))
	}

	// ── Step 2: Guard — keyring must be configured for credential decryption ──
	if h.keyring == nil {
		writeFailure(w, http.StatusOK, "no QRZ credentials configured",
			"store QRZ credentials via POST /v1/credentials with service=qrz")
		return
	}

	// ── Step 3: Get QRZ client for this user (decrypts credentials) ───────────
	client, credErr := h.qrzClientForUser(r, userID)
	if credErr != nil {
		writeFailure(w, http.StatusOK, "callsign not in cache and no QRZ credentials configured",
			"store QRZ credentials via POST /v1/credentials with service=qrz to enable live lookup")
		return
	}

	// ── Step 4: QRZ lookup ────────────────────────────────────────────────────
	info, lookupErr := client.LookupCallsign(ctx, callsign)
	if errors.Is(lookupErr, qrz.ErrNotFound) {
		writeFailure(w, http.StatusOK, "callsign not found", "QRZ has no record for this callsign")
		return
	}
	if errors.Is(lookupErr, qrz.ErrNotSubscribed) {
		writeFailure(w, http.StatusOK, "QRZ subscription required",
			"your QRZ account does not have XML API access; upgrade at qrz.com")
		return
	}
	if lookupErr != nil {
		slog.ErrorContext(ctx, "qrz: lookup failed",
			slog.String("callsign", callsign), slog.String("error", lookupErr.Error()))
		writeFailure(w, http.StatusInternalServerError, "QRZ lookup failed", "external API error")
		return
	}

	// ── Step 5: Cache the result (best-effort, in background) ─────────────────
	// Use context.WithoutCancel so the goroutine outlives the HTTP request while
	// preserving any tracing/logging values from the original context.
	go h.cacheCallsignInfo(context.WithoutCancel(ctx), callsign, info)

	fetchedAt := time.Now().UTC()
	writeSuccess(w, http.StatusOK, "callsign found", callsignInfoFromQRZ(info, "qrz", fetchedAt))
}

// ─────────────────────────────────────────────────────────────────────────────
// GET /v1/callsigns/autocomplete?q=W5X
// ─────────────────────────────────────────────────────────────────────────────

// Autocomplete handles GET /v1/callsigns/autocomplete?q=<prefix>.
//
// Returns up to 10 cached callsigns starting with the given prefix,
// with name and grid for the QSO form type-ahead dropdown.
// Only non-expired cache entries are searched.
func (h *CallsignLookupHandler) Autocomplete(w http.ResponseWriter, r *http.Request) {
	_, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	prefix := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("q")))
	if prefix == "" {
		writeSuccess(w, http.StatusOK, "autocomplete results",
			map[string]any{"items": []autocompleteItem{}})
		return
	}

	// callsign_cache has no RLS — use plain pool connection.
	conn, err := h.pool.Acquire(r.Context())
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable",
			"could not acquire connection")
		return
	}
	defer conn.Release()

	queries := db.New(conn)
	rows, err := queries.AutocompleteCallsigns(r.Context(), prefix)
	if err != nil {
		slog.ErrorContext(r.Context(), "autocomplete: query failed",
			slog.String("error", err.Error()))
		writeFailure(w, http.StatusInternalServerError, "autocomplete failed", "query error")
		return
	}

	items := make([]autocompleteItem, 0, len(rows))
	for _, row := range rows {
		item := autocompleteItem{Callsign: row.Callsign}
		if row.FullName != nil {
			item.FullName = fmt.Sprintf("%v", row.FullName)
		}
		if row.Grid != nil {
			item.Grid = fmt.Sprintf("%v", row.Grid)
		}
		items = append(items, item)
	}

	writeSuccess(w, http.StatusOK, "autocomplete results", map[string]any{"items": items})
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// getCacheEntry fetches a non-expired callsign_cache row using a plain connection.
// callsign_cache has no RLS, so no tenant context is required.
func (h *CallsignLookupHandler) getCacheEntry(ctx context.Context, callsign string) (db.CallsignCache, error) {
	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		return db.CallsignCache{}, fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	queries := db.New(conn)
	return queries.GetCallsignCache(ctx, callsign)
}

// qrzClientForUser returns a cached-or-new QRZ client for the given user.
// On first call per user (or after TTL expiry), decrypts their stored QRZ credentials.
// Evicts the cache entry on credential errors. Returns an error if no QRZ credentials
// are stored.
func (h *CallsignLookupHandler) qrzClientForUser(r *http.Request, userID int64) (*qrz.Client, error) {
	// Check the cache. Evict entries older than qrzClientTTL.
	if v, ok := h.qrzClients.Load(userID); ok {
		entry := v.(*qrzClientEntry)
		entry.mu.Lock()
		age := time.Since(entry.lastUsed)
		if age < qrzClientTTL {
			entry.lastUsed = time.Now()
			client := entry.client
			entry.mu.Unlock()
			return client, nil
		}
		entry.mu.Unlock()
		// TTL expired — evict and fall through to re-create.
		h.qrzClients.Delete(userID)
		slog.DebugContext(r.Context(), "qrz client cache: TTL expired, re-authenticating",
			slog.Int64("user_id", userID))
	}

	// Fetch the encrypted credential row (requires tenant context for RLS).
	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	q := db.New(tx)
	cred, err := q.GetCredential(r.Context(), db.GetCredentialParams{
		UserID:  userID,
		Service: "qrz",
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no QRZ credentials for user %d", userID)
	}
	if err != nil {
		return nil, fmt.Errorf("get credential: %w", err)
	}

	if err := tx.Commit(r.Context()); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	// Decrypt. NEVER log the plaintext.
	plaintext, err := h.keyring.Decrypt(userID, cred.KeyVersion, cred.Credentials)
	if err != nil {
		// Evict any stale entry so next request retries credential fetch.
		h.qrzClients.Delete(userID)
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}

	// Stored format for username_password credentials: "username:password"
	parts := strings.SplitN(string(plaintext), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("qrz credential format invalid (expected username:password separated by ':')")
	}

	client := qrz.New(parts[0], parts[1])

	// Cache with TTL tracking. CompareAndSwap-style: if another goroutine raced
	// and stored first, we accept their entry (both are equivalent fresh clients).
	entry := &qrzClientEntry{client: client, lastUsed: time.Now()}
	actual, _ := h.qrzClients.LoadOrStore(userID, entry)
	return actual.(*qrzClientEntry).client, nil
}

// cacheCallsignInfo stores a QRZ result in callsign_cache.
// Runs in a background goroutine; errors are logged and swallowed.
// callsign_cache has no RLS so no tenant context is required.
// ctx should be context.WithoutCancel(r.Context()) so the goroutine outlives
// the HTTP request without inheriting its cancellation.
func (h *CallsignLookupHandler) cacheCallsignInfo(ctx context.Context, callsign string, info *qrz.CallsignInfo) {

	data, err := json.Marshal(info)
	if err != nil {
		slog.Error("callsign_cache: marshal failed",
			slog.String("callsign", callsign), slog.String("error", err.Error()))
		return
	}

	conn, err := h.pool.Acquire(ctx)
	if err != nil {
		slog.Error("callsign_cache: acquire conn failed",
			slog.String("callsign", callsign), slog.String("error", err.Error()))
		return
	}
	defer conn.Release()

	queries := db.New(conn)
	_, err = queries.UpsertCallsignCache(ctx, db.UpsertCallsignCacheParams{
		Callsign: callsign,
		Data:     data,
		Source:   "qrz",
		ExpiresAt: pgtype.Timestamptz{
			Time:  time.Now().Add(qrz.CacheTTL),
			Valid: true,
		},
	})
	if err != nil {
		slog.Error("callsign_cache: upsert failed",
			slog.String("callsign", callsign), slog.String("error", err.Error()))
	}
}

// callsignInfoFromQRZ converts a qrz.CallsignInfo to the HTTP response shape.
func callsignInfoFromQRZ(info *qrz.CallsignInfo, source string, fetchedAt time.Time) callsignInfoResponse {
	return callsignInfoResponse{
		Callsign:  info.Callsign,
		FullName:  info.FullName,
		FName:     info.FName,
		LName:     info.LName,
		Addr1:     info.Addr1,
		Addr2:     info.Addr2,
		State:     info.State,
		Zip:       info.Zip,
		Country:   info.Country,
		Grid:      info.Grid,
		Lat:       info.Lat,
		Lon:       info.Lon,
		Class:     info.Class,
		Expires:   info.Expires,
		DXCC:      info.DXCC,
		Land:      info.Land,
		CQZone:    info.CQZone,
		ITUZone:   info.ITUZone,
		QSLMgr:    info.QSLMgr,
		Email:     info.Email,
		Image:     info.Image,
		Source:    source,
		FetchedAt: fetchedAt,
	}
}
