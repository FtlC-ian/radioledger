package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/FtlC-ian/radioledger/api/internal/adminaccess"
	"github.com/FtlC-ian/radioledger/api/internal/auth"
	"github.com/FtlC-ian/radioledger/api/internal/config"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	"github.com/FtlC-ian/radioledger/api/internal/middleware"
	"github.com/FtlC-ian/radioledger/api/internal/securitylog"
)

// AuthHandler handles user registration, login, and profile endpoints.
//
// In local mode (AUTH_MODE=local, or the backward-compatible alias AUTH_MODE=dev),
// registration and login are handled directly: passwords are bcrypt-hashed and stored
// in the database, and signed HS256 JWT tokens are issued on success.
//
// In production (AUTH_MODE=zitadel), registration and login are delegated to
// Zitadel's hosted UI — the /register and /login endpoints return 501 Not Implemented,
// and users authenticate through the Zitadel OIDC flow.
type AuthHandler struct {
	pool        *pgxpool.Pool
	cfg         *config.Config
	localMode   bool
	localAuth   *auth.LocalAuth   // non-nil only when localMode == true
	zitadelAuth *auth.ZitadelAuth // non-nil only in Zitadel mode
}

// NewAuthHandler creates an AuthHandler.
// Pass localAuth to enable the local registration/login/change-password endpoints.
// Pass nil for localAuth to operate in Zitadel mode (register/login return 501).
func NewAuthHandler(pool *pgxpool.Pool, cfg *config.Config, localAuth *auth.LocalAuth, zitadelAuth *auth.ZitadelAuth) *AuthHandler {
	return &AuthHandler{
		pool:        pool,
		cfg:         cfg,
		localMode:   localAuth != nil,
		localAuth:   localAuth,
		zitadelAuth: zitadelAuth,
	}
}

// registerRequest is the JSON body for POST /v1/auth/register (local mode only).
//
// NOTE: Callsign and DisplayName are accepted but SHOULD NOT be set by
// frontend registration forms. The onboarding wizard (triggered when
// callsign is empty) collects callsign, grid square, and profile details.
// If callsign is provided at registration time, onboarding is skipped
// and the user misses grid square and other setup steps.
type registerRequest struct {
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	DisplayName *string `json:"display_name"`
	Callsign    *string `json:"callsign"`
	Timezone    *string `json:"timezone"`
	InviteCode  *string `json:"invite_code"`
}

type inviteValidationRequest struct {
	InviteCode string `json:"invite_code"`
}

type updateProfileRequest struct {
	Callsign   *string `json:"callsign"`
	GridSquare *string `json:"grid_square"`
}

// loginRequest is the JSON body for POST /v1/auth/login (local mode only).
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// changePasswordRequest is the JSON body for POST /v1/auth/change-password (local mode only).
type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// authResponse is returned by register and login endpoints.
type authResponse struct {
	Token string      `json:"token"`
	User  userProfile `json:"user"`
}

// userProfile is the shape returned by GET /v1/auth/me and embedded in authResponse.
type userProfile struct {
	UUID               string    `json:"uuid"`
	Email              string    `json:"email"`
	DisplayName        *string   `json:"display_name,omitempty"`
	Callsign           *string   `json:"callsign,omitempty"`
	GridSquare         *string   `json:"grid_square,omitempty"`
	OnboardingComplete bool      `json:"onboarding_complete"`
	Timezone           string    `json:"timezone"`
	CreatedAt          time.Time `json:"created_at"`
	IsAdmin            bool      `json:"is_admin"`
}

func (h *AuthHandler) workerQueries(ctx context.Context) (*db.Queries, func(), error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE radioledger_worker"); err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	return db.New(tx), func() { _ = tx.Rollback(ctx) }, nil
}

func bearerTokenFromRequest(r *http.Request) (string, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", false
	}
	token := strings.TrimSpace(authHeader[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func normalizeUpperOptional(v *string) *string {
	normalized := normalizeOptional(v)
	if normalized == nil {
		return nil
	}
	upper := strings.ToUpper(*normalized)
	return &upper
}

func normalizeCallsign(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func normalizeGridSquare(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

func validateCallsignFormat(callsign string) error {
	callsign = normalizeCallsign(callsign)
	if callsign == "" {
		return fmt.Errorf("callsign is required")
	}
	if len(callsign) < 3 || len(callsign) > 10 {
		return fmt.Errorf("callsign must be 3-10 characters")
	}
	if !callsignPattern.MatchString(callsign) {
		return fmt.Errorf("callsign must look like a valid amateur radio callsign")
	}

	hasLetter := false
	for _, r := range callsign {
		if r >= 'A' && r <= 'Z' {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return fmt.Errorf("callsign must contain letters")
	}

	return nil
}

func validateGridSquareFormat(gridSquare *string) error {
	if gridSquare == nil {
		return nil
	}
	grid := normalizeGridSquare(*gridSquare)
	if grid == "" {
		return nil
	}
	if !maidenheadPattern.MatchString(grid) {
		return fmt.Errorf("grid_square must be a valid Maidenhead locator (for example EM35 or EM35FX)")
	}
	return nil
}

func (h *AuthHandler) callsignInUse(ctx context.Context, callsign string, excludeUserID int64) (bool, error) {
	var ownerID int64
	err := h.pool.QueryRow(ctx, `
		SELECT id
		FROM users
		WHERE UPPER(callsign) = UPPER($1)
		  AND deleted_at IS NULL
		LIMIT 1
	`, callsign).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return ownerID != excludeUserID, nil
}

const bcryptCost = 12
const minPasswordLen = 8
const maxPasswordLen = 128

var (
	callsignPattern   = regexp.MustCompile(`^[A-Z0-9]{1,3}[0-9][A-Z0-9]{1,6}$`)
	maidenheadPattern = regexp.MustCompile(`^[A-R]{2}[0-9]{2}([A-X]{2})?$`)
)

const (
	maxFailedLoginAttempts = 5
	loginLockoutDuration   = 15 * time.Minute
	loginCleanupInterval   = 5 * time.Minute
	loginEntryTTL          = 30 * time.Minute
)

type loginAttemptState struct {
	mu          sync.Mutex
	count       int
	lockedUntil time.Time
	updatedAt   time.Time
}

var loginAttemptTracker sync.Map // map[email]*loginAttemptState

func init() {
	go cleanupLoginAttemptTracker()
}

// Register handles POST /v1/auth/register.
//
// Local mode: Creates a new user with a bcrypt-hashed password, creates a default
// logbook, and returns a signed JWT bearer token.
//
// Production (Zitadel) mode: Returns 501 Not Implemented. Registration is handled
// by the Zitadel hosted UI.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.localMode {
		writeFailure(w, http.StatusNotImplemented, "not implemented",
			"user registration is managed by Zitadel in production mode; use the Zitadel UI to register")
		return
	}

	var req registerRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "email is required")
		return
	}

	inviteCode := normalizeInviteCode(valueOrEmpty(req.InviteCode))
	if h.cfg != nil && h.cfg.RequireInviteKey {
		if inviteCode == "" {
			writeFailure(w, http.StatusForbidden, "registration failed", "invite required")
			return
		}
		if !isValidInviteCodeFormat(inviteCode) {
			writeFailure(w, http.StatusForbidden, "registration failed", "invalid invite code")
			return
		}
	}

	if len(req.Password) < minPasswordLen {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"password must be at least 8 characters")
		return
	}
	if len(req.Password) > maxPasswordLen {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"password must be at most 128 characters")
		return
	}

	normalizedCallsign := normalizeUpperOptional(req.Callsign)
	if normalizedCallsign != nil {
		if err := validateCallsignFormat(*normalizedCallsign); err != nil {
			writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
			return
		}
		inUse, err := h.callsignInUse(r.Context(), *normalizedCallsign, 0)
		if err != nil {
			writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to validate callsign")
			return
		}
		if inUse {
			writeFailure(w, http.StatusConflict, "registration failed", "callsign is already in use")
			return
		}
	}

	// Hash password with bcrypt.
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "registration failed", "failed to hash password")
		return
	}
	passwordHash := string(hashBytes)

	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to begin transaction")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := db.New(tx)

	// Check for duplicate email.
	_, existErr := queries.GetUserByEmail(ctx, email)
	if existErr == nil {
		writeFailure(w, http.StatusConflict, "registration failed", "an account with this email already exists")
		return
	}
	if !errors.Is(existErr, pgx.ErrNoRows) {
		writeFailure(w, http.StatusInternalServerError, "registration failed", "database error")
		return
	}

	// Create the user account with hashed password.
	row, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		PasswordHash: &passwordHash,
		Callsign:     normalizedCallsign,
		DisplayName:  req.DisplayName,
		Timezone:     req.Timezone,
	})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "registration failed", "failed to create user")
		return
	}

	if h.cfg != nil && h.cfg.RequireInviteKey {
		if _, err := queries.ConsumeInviteByCode(ctx, db.ConsumeInviteByCodeParams{
			UsedBy: &row.ID,
			Code:   inviteCode,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeFailure(w, http.StatusForbidden, "registration failed", "invalid or expired invite code")
				return
			}
			writeFailure(w, http.StatusInternalServerError, "registration failed", "failed to consume invite")
			return
		}
	}

	// Create a default logbook for the new user.
	if err := auth.CreateDefaultLogbookForUser(ctx, tx, row.ID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "registration failed", "failed to create default logbook")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeFailure(w, http.StatusInternalServerError, "registration failed", "transaction failed")
		return
	}

	token, err := h.localAuth.IssueToken(row.ID, row.Email)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "registration failed", "failed to issue token")
		return
	}

	writeSuccess(w, http.StatusCreated, "user registered", authResponse{
		Token: token,
		User: userProfile{
			UUID:               row.Uuid.String(),
			Email:              row.Email,
			DisplayName:        row.DisplayName,
			Callsign:           row.Callsign,
			OnboardingComplete: row.OnboardingComplete,
			Timezone:           row.Timezone,
			CreatedAt:          row.CreatedAt.Time.UTC(),
			IsAdmin:            adminaccess.IsAdminEmail(row.Email, h.cfg.AdminEmails),
		},
	})
}

// ValidateInvite handles POST /v1/auth/validate-invite.
// It checks whether an invite exists and can still be consumed without mutating it.
func (h *AuthHandler) ValidateInvite(w http.ResponseWriter, r *http.Request) {
	var req inviteValidationRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	if h.cfg == nil || !h.cfg.RequireInviteKey {
		writeSuccess(w, http.StatusOK, "invite validation skipped", map[string]any{"valid": true, "required": false})
		return
	}

	inviteCode := normalizeInviteCode(req.InviteCode)
	if inviteCode == "" {
		writeFailure(w, http.StatusForbidden, "invite required", "invite required")
		return
	}
	if !isValidInviteCodeFormat(inviteCode) {
		writeFailure(w, http.StatusForbidden, "invalid invite", "invalid invite code")
		return
	}

	queries, cleanup, err := h.workerQueries(r.Context())
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to validate invite")
		return
	}
	defer cleanup()

	invite, err := queries.GetActiveInviteByCode(r.Context(), inviteCode)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusForbidden, "invalid invite", "invalid or expired invite code")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to validate invite", "database error")
		return
	}

	writeSuccess(w, http.StatusOK, "invite is valid", map[string]any{
		"valid":    true,
		"required": true,
		"invite":   inviteKeyResponseFromModel(invite),
	})
}

// ConsumeInvite handles POST /v1/auth/consume-invite for first-time Zitadel sign-ins.
func (h *AuthHandler) ConsumeInvite(w http.ResponseWriter, r *http.Request) {
	if h.zitadelAuth == nil {
		writeFailure(w, http.StatusNotImplemented, "not implemented", "invite consumption is only supported for Zitadel authentication")
		return
	}

	var req inviteValidationRequest
	if err := decodeOptionalJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	bearerToken, ok := bearerTokenFromRequest(r)
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return
	}

	identity, err := h.zitadelAuth.ResolveIdentityFromToken(r.Context(), bearerToken)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
		return
	}

	// Validate invite code format early (before opening a transaction) so we can
	// fail fast without a round-trip to the database.
	inviteRequired := h.cfg != nil && h.cfg.RequireInviteKey
	var inviteCode string
	if inviteRequired {
		inviteCode = normalizeInviteCode(req.InviteCode)
		if inviteCode == "" {
			writeFailure(w, http.StatusForbidden, "invite required", "invite required")
			return
		}
		if !isValidInviteCodeFormat(inviteCode) {
			writeFailure(w, http.StatusForbidden, "invalid invite", "invalid invite code")
			return
		}
	}

	// Open a single transaction that covers the existence check, user creation,
	// invite consumption, and default logbook creation.  Performing all steps
	// inside one transaction eliminates the race condition where two concurrent
	// requests with the same Zitadel token could each pass the "user not found"
	// check and both attempt to create the same user / consume the same invite.
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to provision user")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if _, err := tx.Exec(r.Context(), "SET LOCAL ROLE radioledger_worker"); err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to provision user")
		return
	}

	queries := db.New(tx)
	zitadelID := identity.Subject

	// Check for an existing user within this transaction to prevent double-provisioning.
	existingRow, err := queries.GetUserByZitadelID(r.Context(), &zitadelID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusInternalServerError, "failed to consume invite", "user lookup failed")
		return
	}
	if err == nil {
		// User already provisioned — commit (no changes) and return success.
		if commitErr := tx.Commit(r.Context()); commitErr != nil {
			writeFailure(w, http.StatusInternalServerError, "failed to consume invite", "transaction failed")
			return
		}
		writeSuccess(w, http.StatusOK, "user already provisioned", map[string]any{"provisioned": true, "email": existingRow.Email})
		return
	}

	row, err := queries.CreateZitadelUser(r.Context(), db.CreateZitadelUserParams{Email: identity.Email, ZitadelID: &zitadelID})
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to provision user", "failed to create user")
		return
	}

	if inviteRequired {
		if _, err := queries.ConsumeInviteByCode(r.Context(), db.ConsumeInviteByCodeParams{UsedBy: &row.ID, Code: inviteCode}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeFailure(w, http.StatusForbidden, "invalid invite", "invalid or expired invite code")
				return
			}
			writeFailure(w, http.StatusInternalServerError, "failed to consume invite", "failed to consume invite")
			return
		}
	}

	if err := auth.CreateDefaultLogbookForUser(r.Context(), tx, row.ID); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to provision user", "failed to create default logbook")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to provision user", "transaction failed")
		return
	}

	msg := "user provisioned"
	if inviteRequired {
		msg = "invite consumed"
	}
	writeSuccess(w, http.StatusOK, msg, userProfile{
		UUID:               row.Uuid.String(),
		Email:              row.Email,
		OnboardingComplete: row.OnboardingComplete,
		Timezone:           row.Timezone,
		CreatedAt:          row.CreatedAt.Time.UTC(),
		IsAdmin:            adminaccess.IsAdminEmail(row.Email, h.cfg.AdminEmails),
	})
}

// Login handles POST /v1/auth/login.
//
// Local mode: Looks up the user by email, verifies the bcrypt password, and
// returns a signed JWT bearer token.
//
// Production (Zitadel) mode: Returns 501. Authentication is via OIDC.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.localMode {
		writeFailure(w, http.StatusNotImplemented, "not implemented",
			"login is managed by Zitadel in production mode; use the OIDC authorization flow")
		return
	}

	var req loginRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "email is required")
		return
	}
	if req.Password == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "password is required")
		return
	}
	if len(req.Password) > maxPasswordLen {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"password must be at most 128 characters")
		return
	}

	now := time.Now()
	if lockedUntil, locked := loginLockedUntil(email, now); locked {
		minutes := int(math.Ceil(time.Until(lockedUntil).Minutes()))
		if minutes < 1 {
			minutes = 1
		}
		writeFailure(w, http.StatusTooManyRequests, "too many requests",
			fmt.Sprintf("Too many login attempts, try again in %d minutes", minutes))
		return
	}

	ctx := r.Context()
	queries := db.New(h.pool)
	row, err := queries.GetUserByEmailWithPassword(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		recordFailedLoginAttempt(email, now)
		securitylog.Event(r.Context(), "failed_login",
			slog.String("email", email),
			slog.String("remote_ip", middleware.ClientIP(r)),
		)
		writeFailure(w, http.StatusUnauthorized, "login failed", "invalid credentials")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "login failed", "database error")
		return
	}

	// Verify password. Use a generic error to avoid user enumeration.
	if row.PasswordHash == nil {
		recordFailedLoginAttempt(email, now)
		securitylog.Event(r.Context(), "failed_login",
			slog.String("email", email),
			slog.String("reason", "no_password_set"),
			slog.String("remote_ip", middleware.ClientIP(r)),
		)
		writeFailure(w, http.StatusUnauthorized, "login failed", "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*row.PasswordHash), []byte(req.Password)); err != nil {
		recordFailedLoginAttempt(email, now)
		securitylog.Event(r.Context(), "failed_login",
			slog.String("email", email),
			slog.String("remote_ip", middleware.ClientIP(r)),
		)
		writeFailure(w, http.StatusUnauthorized, "login failed", "invalid credentials")
		return
	}

	resetLoginAttempts(email)

	token, err := h.localAuth.IssueToken(row.ID, row.Email)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "login failed", "failed to issue token")
		return
	}

	writeSuccess(w, http.StatusOK, "login successful", authResponse{
		Token: token,
		User: userProfile{
			UUID:               row.Uuid.String(),
			Email:              row.Email,
			DisplayName:        row.DisplayName,
			Callsign:           row.Callsign,
			OnboardingComplete: row.OnboardingComplete,
			Timezone:           row.Timezone,
			CreatedAt:          row.CreatedAt.Time.UTC(),
			IsAdmin:            adminaccess.IsAdminEmail(row.Email, h.cfg.AdminEmails),
		},
	})
}

func loginAttemptStateFor(email string) *loginAttemptState {
	if value, ok := loginAttemptTracker.Load(email); ok {
		return value.(*loginAttemptState)
	}
	state := &loginAttemptState{}
	actual, _ := loginAttemptTracker.LoadOrStore(email, state)
	return actual.(*loginAttemptState)
}

func loginLockedUntil(email string, now time.Time) (time.Time, bool) {
	value, ok := loginAttemptTracker.Load(email)
	if !ok {
		return time.Time{}, false
	}
	state := value.(*loginAttemptState)
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.lockedUntil.After(now) {
		state.updatedAt = now
		return state.lockedUntil, true
	}
	if !state.lockedUntil.IsZero() {
		state.lockedUntil = time.Time{}
		state.count = 0
	}
	return time.Time{}, false
}

func recordFailedLoginAttempt(email string, now time.Time) {
	state := loginAttemptStateFor(email)
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.lockedUntil.After(now) {
		state.updatedAt = now
		return
	}

	state.count++
	state.updatedAt = now
	if state.count >= maxFailedLoginAttempts {
		state.lockedUntil = now.Add(loginLockoutDuration)
		state.count = 0
	}
}

func resetLoginAttempts(email string) {
	loginAttemptTracker.Delete(email)
}

func cleanupLoginAttemptTracker() {
	ticker := time.NewTicker(loginCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		loginAttemptTracker.Range(func(key, value any) bool {
			state, ok := value.(*loginAttemptState)
			if !ok {
				loginAttemptTracker.Delete(key)
				return true
			}

			state.mu.Lock()
			stale := now.Sub(state.updatedAt) > loginEntryTTL && state.lockedUntil.Before(now)
			state.mu.Unlock()

			if stale {
				loginAttemptTracker.Delete(key)
			}
			return true
		})
	}
}

// ChangePassword handles POST /v1/auth/change-password.
//
// Local mode only. Requires authentication. Verifies old_password against the stored
// bcrypt hash, then updates the hash with the new bcrypt-hashed password.
//
// Returns 501 in Zitadel mode (password management is Zitadel's responsibility).
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if !h.localMode {
		writeFailure(w, http.StatusNotImplemented, "not implemented",
			"password management is handled by Zitadel in production mode")
		return
	}

	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req changePasswordRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}
	if req.OldPassword == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "old_password is required")
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"new_password must be at least 8 characters")
		return
	}
	if len(req.NewPassword) > maxPasswordLen {
		writeFailure(w, http.StatusBadRequest, "invalid request",
			"new_password must be at most 128 characters")
		return
	}

	ctx := r.Context()
	queries := db.New(h.pool)

	// Fetch the current password hash.
	var currentHash *string
	err = h.pool.QueryRow(ctx,
		`SELECT password_hash FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&currentHash)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "change password failed", "database error")
		return
	}
	if currentHash == nil {
		writeFailure(w, http.StatusBadRequest, "change password failed",
			"no password is set on this account; use register to set one")
		return
	}

	// Verify the old password.
	if err := bcrypt.CompareHashAndPassword([]byte(*currentHash), []byte(req.OldPassword)); err != nil {
		securitylog.Event(ctx, "failed_change_password",
			slog.Int64("user_id", userID),
		)
		writeFailure(w, http.StatusUnauthorized, "change password failed", "old password is incorrect")
		return
	}

	// Hash and store the new password.
	newHashBytes, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcryptCost)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "change password failed", "failed to hash password")
		return
	}

	newHash := string(newHashBytes)
	if err := queries.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{
		PasswordHash: &newHash,
		ID:           userID,
	}); err != nil {
		writeFailure(w, http.StatusInternalServerError, "change password failed", "database error")
		return
	}

	writeSuccess(w, http.StatusOK, "password updated", nil)
}

// CallsignAvailability handles GET /v1/auth/callsign-availability.
// It returns whether a callsign can be claimed by the authenticated user.
func (h *AuthHandler) CallsignAvailability(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	callsign := normalizeCallsign(r.URL.Query().Get("callsign"))
	if callsign == "" {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}

	if err := validateCallsignFormat(callsign); err != nil {
		writeSuccess(w, http.StatusOK, "callsign unavailable", map[string]any{
			"callsign":  callsign,
			"available": false,
			"reason":    err.Error(),
		})
		return
	}

	inUse, err := h.callsignInUse(r.Context(), callsign, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to validate callsign")
		return
	}

	payload := map[string]any{
		"callsign":  callsign,
		"available": !inUse,
	}
	if inUse {
		payload["reason"] = "callsign is already in use"
		writeSuccess(w, http.StatusOK, "callsign unavailable", payload)
		return
	}

	writeSuccess(w, http.StatusOK, "callsign available", payload)
}

// UpdateProfile handles PATCH /v1/auth/profile.
// It completes or updates the authenticated user's core ham profile.
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	var req updateProfileRequest
	if err := decodeJSONBody(r, &req); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "invalid JSON body")
		return
	}

	callsign := normalizeUpperOptional(req.Callsign)
	if callsign == nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", "callsign is required")
		return
	}
	if err := validateCallsignFormat(*callsign); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	gridSquare := normalizeUpperOptional(req.GridSquare)
	if err := validateGridSquareFormat(gridSquare); err != nil {
		writeFailure(w, http.StatusBadRequest, "invalid request", err.Error())
		return
	}

	inUse, err := h.callsignInUse(r.Context(), *callsign, userID)
	if err != nil {
		writeFailure(w, http.StatusServiceUnavailable, "database unavailable", "failed to validate callsign")
		return
	}
	if inUse {
		writeFailure(w, http.StatusConflict, "profile update failed", "callsign is already in use")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "profile update failed", "database error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var profile userProfile
	err = tx.QueryRow(r.Context(), `
		UPDATE users
		SET callsign = $2,
		    grid_square = $3,
		    onboarding_complete = TRUE,
		    updated_at = NOW()
		WHERE id = $1
		  AND deleted_at IS NULL
		RETURNING uuid, email, display_name, callsign, grid_square, onboarding_complete, timezone, created_at
	`, userID, *callsign, gridSquare).Scan(
		&profile.UUID,
		&profile.Email,
		&profile.DisplayName,
		&profile.Callsign,
		&profile.GridSquare,
		&profile.OnboardingComplete,
		&profile.Timezone,
		&profile.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusNotFound, "profile update failed", "user not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "profile update failed", "database error")
		return
	}

	profile.CreatedAt = profile.CreatedAt.UTC()
	profile.IsAdmin = adminaccess.IsAdminEmail(profile.Email, h.cfg.AdminEmails)

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "profile update failed", "database error")
		return
	}

	writeSuccess(w, http.StatusOK, "profile updated", profile)
}

// Me handles GET /v1/auth/me.
// Returns the authenticated user's profile. Requires a valid bearer token.
// Works in both local and production modes.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	info, ok := auth.UserInfoFromContext(r.Context())
	if !ok {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "not authenticated")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, info.UserID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch user", "database error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var profile userProfile
	err = tx.QueryRow(r.Context(), `
		SELECT uuid, email, display_name, callsign, grid_square, onboarding_complete, timezone, created_at
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
	`, info.UserID).Scan(
		&profile.UUID,
		&profile.Email,
		&profile.DisplayName,
		&profile.Callsign,
		&profile.GridSquare,
		&profile.OnboardingComplete,
		&profile.Timezone,
		&profile.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "user not found", "user not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch user", "database error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to fetch user", "database error")
		return
	}

	profile.CreatedAt = profile.CreatedAt.UTC()
	profile.IsAdmin = adminaccess.IsAdminEmail(profile.Email, h.cfg.AdminEmails)

	writeSuccess(w, http.StatusOK, "user retrieved", profile)
}

// DeleteMe handles DELETE /v1/auth/me.
// Performs a soft delete by setting users.deleted_at.
func (h *AuthHandler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := db.New(tx)
	affected, err := queries.SoftDeleteUserByID(r.Context(), userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "delete failed", "database error")
		return
	}
	if affected == 0 {
		writeFailure(w, http.StatusNotFound, "not found", "user not found")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "delete failed", "database error")
		return
	}

	writeSuccess(w, http.StatusOK, "account scheduled for deletion", nil)
}
