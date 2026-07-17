// Package confirmation implements the QSO confirmation matching engine for RadioLedger.
//
// The matching engine finds QSOs from other users that correspond to the same
// on-air contact. Two QSOs match when:
//
//   - Callsign pair: A logged B, and B logged A
//   - Same band
//   - Same mode group (with fuzzy normalization: USB/LSB→SSB, CW/CWR→CW, etc.)
//   - Time within ±30 minutes (configurable)
//
// Cross-tenant queries use the find_qso_matches() SECURITY DEFINER function
// in the database, which intentionally bypasses RLS for this operation.
package confirmation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/pgtype"
)

// DefaultTimeWindow is the default ±window used for matching QSO times.
const DefaultTimeWindow = 30 * time.Minute

// MatchCandidate holds a potential matching QSO from another user.
type MatchCandidate struct {
	QSOID      int64
	UserID     int64
	TheirCallsign string // the other station's callsign as logged by them (= our callsign)
	OurCallsign   string // station_callsign from their log (may be empty)
	Band       string
	Mode       string
	DatetimeOn time.Time
	Confidence float64 // 1.0 = exact mode match, 0.9 = fuzzy mode match
}

// MatchRequest describes a QSO for which we want to find matches.
type MatchRequest struct {
	// QSOID is the ID of the QSO we're matching for.
	QSOID int64
	// UserID is the owner of the QSO (excluded from results).
	UserID int64
	// OurCallsign is the station that logged the QSO (the operator's callsign).
	OurCallsign string
	// TheirCallsign is the callsign of the worked station.
	TheirCallsign string
	// Band is the band (e.g. "20m").
	Band string
	// Mode is the raw mode string (e.g. "USB", "FT8").
	Mode string
	// DatetimeOn is the QSO time (UTC).
	DatetimeOn time.Time
	// TimeWindow overrides DefaultTimeWindow when non-zero.
	TimeWindow time.Duration
}

// NormalizeModeGroup maps a raw mode to its matching group.
// Fuzzy groups: USB/LSB/AM → SSB, CW/CWR → CW, RTTY/BAUDOT → RTTY.
// Other modes match exactly.
func NormalizeModeGroup(mode string) string {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "USB", "LSB", "AM", "SSB":
		return "SSB"
	case "CW", "CWR":
		return "CW"
	case "RTTY", "BAUDOT":
		return "RTTY"
	default:
		// FT8, FT4, JS8, DMR, etc. match exactly
		return strings.ToUpper(strings.TrimSpace(mode))
	}
}

// FindMatches searches for QSOs from other users that match the given QSO.
// Uses the find_qso_matches() SECURITY DEFINER function for cross-tenant access.
func FindMatches(ctx context.Context, pool *pgxpool.Pool, req MatchRequest) ([]MatchCandidate, error) {
	window := req.TimeWindow
	if window <= 0 {
		window = DefaultTimeWindow
	}

	modeGroup := NormalizeModeGroup(req.Mode)

	rows, err := pool.Query(ctx,
		`SELECT qso_id, user_id, their_callsign, our_callsign, band, mode, datetime_on, confidence
		 FROM find_qso_matches($1, $2, $3, $4, $5, $6, $7)`,
		req.OurCallsign,
		req.TheirCallsign,
		req.Band,
		modeGroup,
		pgtype.Timestamptz{Time: req.DatetimeOn.UTC(), Valid: true},
		fmt.Sprintf("%d seconds", int(window.Seconds())),
		req.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("find_qso_matches: %w", err)
	}
	defer rows.Close()

	var results []MatchCandidate
	for rows.Next() {
		var c MatchCandidate
		var dtOn pgtype.Timestamptz
		var ourCallsign *string
		if err := rows.Scan(
			&c.QSOID,
			&c.UserID,
			&c.TheirCallsign,
			&ourCallsign,
			&c.Band,
			&c.Mode,
			&dtOn,
			&c.Confidence,
		); err != nil {
			return nil, fmt.Errorf("scan match row: %w", err)
		}
		if dtOn.Valid {
			c.DatetimeOn = dtOn.Time
		}
		if ourCallsign != nil {
			c.OurCallsign = *ourCallsign
		}
		results = append(results, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match rows: %w", err)
	}

	return results, nil
}

// BestMatch returns the highest-confidence candidate, or nil if there are none.
func BestMatch(candidates []MatchCandidate) *MatchCandidate {
	if len(candidates) == 0 {
		return nil
	}
	best := &candidates[0]
	for i := range candidates[1:] {
		c := &candidates[i+1]
		if c.Confidence > best.Confidence {
			best = c
		}
	}
	return best
}

// OperatorVerificationLevel returns the highest verification level for a user+callsign pair.
// Returns "none" if no verified record exists.
func OperatorVerificationLevel(ctx context.Context, pool *pgxpool.Pool, userID int64, callsign string) (string, error) {
	// Priority order (highest to lowest)
	levels := []string{"cross_verified", "vouched", "address", "email"}
	for _, level := range levels {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM operator_verifications
				WHERE user_id = $1
				  AND upper(callsign) = upper($2)
				  AND method = $3
				  AND status = 'verified'
				  AND (expires_at IS NULL OR expires_at > now())
			)
		`, userID, callsign, levelToMethod(level)).Scan(&exists)
		if err != nil {
			return "none", fmt.Errorf("check verification level: %w", err)
		}
		if exists {
			return level, nil
		}
	}
	return "none", nil
}

// levelToMethod maps a verification level to the method stored in the DB.
func levelToMethod(level string) string {
	switch level {
	case "cross_verified":
		// cross_verified can come from lotw_cross or qrz_cross — check either
		return "lotw_cross"
	case "email":
		return "email"
	case "address":
		return "address"
	case "vouched":
		return "vouch"
	default:
		return level
	}
}

// HasCrossVerification checks both cross-verification methods.
func HasCrossVerification(ctx context.Context, pool *pgxpool.Pool, userID int64, callsign string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM operator_verifications
			WHERE user_id = $1
			  AND upper(callsign) = upper($2)
			  AND method IN ('lotw_cross', 'qrz_cross')
			  AND status = 'verified'
			  AND (expires_at IS NULL OR expires_at > now())
		)
	`, userID, callsign).Scan(&exists)
	return exists, err
}

// DetermineConfirmationStatus computes the status for a new confirmation record
// given the verification levels of both sides.
func DetermineConfirmationStatus(ourVerification, theirVerification string, matched bool) string {
	if !matched {
		return "unconfirmed"
	}
	if ourVerification != "none" && theirVerification != "none" {
		return "confirmed"
	}
	return "matched"
}
