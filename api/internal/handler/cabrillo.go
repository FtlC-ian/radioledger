package handler

// cabrillo.go — Cabrillo v3.0 export for contest sessions.
//
// Cabrillo is the standard contest log submission format used by ARRL, CQ Magazine,
// and most major contest sponsors. Format spec: https://wwrof.org/cabrillo/
//
// Format overview:
//   START-OF-LOG: 3.0
//   CALLSIGN: W1AW
//   CONTEST: CQ-WW-SSB
//   CATEGORY-OPERATOR: SINGLE-OP
//   ...header fields...
//   QSO: 14025 CW 2026-10-24 1400 W1AW         001 AR   K1ZZ        023 CT
//   QSO: ...
//   END-OF-LOG:
//
// QSO line format (fixed columns):
//   freq  mode  date        time  sent-call    sent-exch   rcvd-call    rcvd-exch
//   14025 CW    2026-10-24  1400  W1AW         001 AR      K1ZZ         023 CT

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

// CabrilloHandler handles Cabrillo and ADIF export for contest sessions.
type CabrilloHandler struct {
	pool *pgxpool.Pool
}

// NewCabrilloHandler creates a CabrilloHandler.
func NewCabrilloHandler(pool *pgxpool.Pool) *CabrilloHandler {
	return &CabrilloHandler{pool: pool}
}

// ExportCabrillo handles GET /v1/contests/{uuid}/export/cabrillo.
// Returns a Cabrillo 3.0 formatted text file for contest log submission.
func (h *CabrilloHandler) ExportCabrillo(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUIDForCabrillo(r)
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

	// Load header fields.
	header, err := queries.GetContestForExport(r.Context(), sessionUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "contest session not found", "not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "export failed", "header query failed")
		return
	}

	// Load all QSOs (non-dupes only for the official submission).
	qsos, err := queries.GetContestQSOsForExport(r.Context(), sessionUUID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "export failed", "qso query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "export failed", "transaction failed")
		return
	}

	// Build Cabrillo content.
	cabrillo := buildCabrillo(header, qsos)

	// Determine filename from callsign and contest.
	callsign := "NOCALL"
	if header.MyCallsign != nil && strings.TrimSpace(*header.MyCallsign) != "" {
		callsign = strings.ToUpper(strings.TrimSpace(*header.MyCallsign))
	}
	contestCode := strings.ReplaceAll(strings.ToUpper(header.ContestCode), " ", "-")
	filename := fmt.Sprintf("%s-%s.log", callsign, contestCode)

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(cabrillo))
}

// ExportADIF handles GET /v1/contests/{uuid}/export/adif.
// Returns an ADIF file with all contest QSOs including exchange fields.
func (h *CabrilloHandler) ExportADIF(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	sessionUUID, err := parseContestUUIDForCabrillo(r)
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

	header, err := queries.GetContestForExport(r.Context(), sessionUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusOK, "contest session not found", "not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "export failed", "header query failed")
		return
	}

	qsos, err := queries.GetContestQSOsForExport(r.Context(), sessionUUID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "export failed", "qso query failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "export failed", "transaction failed")
		return
	}

	adif := buildContestADIF(header, qsos)

	callsign := "NOCALL"
	if header.MyCallsign != nil && strings.TrimSpace(*header.MyCallsign) != "" {
		callsign = strings.ToUpper(strings.TrimSpace(*header.MyCallsign))
	}
	contestCode := strings.ReplaceAll(strings.ToUpper(header.ContestCode), " ", "-")
	filename := fmt.Sprintf("%s-%s.adi", callsign, contestCode)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(adif))
}

// ─── Cabrillo builder ──────────────────────────────────────────────────────

func buildCabrillo(header db.GetContestForExportRow, qsos []db.GetContestQSOsForExportRow) string {
	var b strings.Builder

	version := coalesceString(header.CabrilloVersion, "3.0")
	_, _ = fmt.Fprintf(&b, "START-OF-LOG: %s\n", version)
	b.WriteString("CREATED-BY: RadioLedger (https://radioledger.app)\n")

	_, _ = fmt.Fprintf(&b, "CONTEST: %s\n", header.ContestCabrilloName)
	callsign := "NOCALL"
	if header.MyCallsign != nil && strings.TrimSpace(*header.MyCallsign) != "" {
		callsign = strings.ToUpper(strings.TrimSpace(*header.MyCallsign))
	}
	_, _ = fmt.Fprintf(&b, "CALLSIGN: %s\n", callsign)
	_, _ = fmt.Fprintf(&b, "CATEGORY-OPERATOR: %s\n", header.CategoryOperator)
	_, _ = fmt.Fprintf(&b, "CATEGORY-ASSISTED: %s\n", header.CategoryAssisted)
	_, _ = fmt.Fprintf(&b, "CATEGORY-BAND: %s\n", header.CategoryBand)
	_, _ = fmt.Fprintf(&b, "CATEGORY-MODE: %s\n", header.CategoryMode)
	_, _ = fmt.Fprintf(&b, "CATEGORY-POWER: %s\n", header.CategoryPower)
	_, _ = fmt.Fprintf(&b, "CATEGORY-STATION: %s\n", header.CategoryStation)
	_, _ = fmt.Fprintf(&b, "CATEGORY-TIME: %s\n", header.CategoryTime)
	_, _ = fmt.Fprintf(&b, "CATEGORY-TRANSMITTER: %s\n", header.CategoryTransmitter)
	if header.CategoryOverlay != nil && strings.TrimSpace(*header.CategoryOverlay) != "" {
		_, _ = fmt.Fprintf(&b, "CATEGORY-OVERLAY: %s\n", *header.CategoryOverlay)
	}

	if header.ClubName != nil && strings.TrimSpace(*header.ClubName) != "" {
		_, _ = fmt.Fprintf(&b, "CLUB: %s\n", *header.ClubName)
	}

	if header.OperatorsLine != nil && strings.TrimSpace(*header.OperatorsLine) != "" {
		_, _ = fmt.Fprintf(&b, "OPERATORS: %s\n", *header.OperatorsLine)
	}

	if header.Location != nil && strings.TrimSpace(*header.Location) != "" {
		_, _ = fmt.Fprintf(&b, "LOCATION: %s\n", *header.Location)
	}

	if header.ClaimedScore != nil && *header.ClaimedScore > 0 {
		_, _ = fmt.Fprintf(&b, "CLAIMED-SCORE: %d\n", *header.ClaimedScore)
	}

	if header.Soapbox != nil && strings.TrimSpace(*header.Soapbox) != "" {
		// Soapbox must be multiple SOAPBOX: lines of ≤ 75 chars each.
		for _, line := range wrapSoapbox(*header.Soapbox, 75) {
			_, _ = fmt.Fprintf(&b, "SOAPBOX: %s\n", line)
		}
	}

	// QSO records — non-dupes only.
	for _, qso := range qsos {
		if qso.IsDupe {
			continue
		}
		line := formatCabrilloQSOLine(callsign, header, qso)
		_, _ = fmt.Fprintf(&b, "QSO: %s\n", line)
	}

	b.WriteString("END-OF-LOG:\n")
	return b.String()
}

// formatCabrilloQSOLine produces a single Cabrillo QSO: line.
//
// Format: freq mode date time sent-call sent-exch rcvd-call rcvd-exch
//
// Example: 14025 CW 2026-10-24 1400 W1AW 001 AR K1ZZ 023 CT
func formatCabrilloQSOLine(myCallsign string, header db.GetContestForExportRow, qso db.GetContestQSOsForExportRow) string {
	// Frequency: convert Hz to kHz (nearest integer).
	var freq string
	if qso.FrequencyHz != nil {
		khz := int(math.Round(float64(*qso.FrequencyHz) / 1000.0))
		freq = fmt.Sprintf("%d", khz)
	} else {
		// Derive from band if no explicit frequency.
		freq = bandToFrequency(qso.Band)
	}

	// Mode: Cabrillo uses CW, PH (phone), RY (RTTY), DG (digital).
	mode := modeForCabrillo(qso.Mode)

	// Date and time in UTC.
	dt := qso.DatetimeOn.Time.UTC()
	date := dt.Format("2006-01-02")
	timeStr := dt.Format("1504")

	// Sent exchange.
	sentExch := ""
	if qso.SentExchange != nil {
		sentExch = strings.TrimSpace(*qso.SentExchange)
	}
	if sentExch == "" && header.ExchangeSent != nil {
		sentExch = strings.TrimSpace(*header.ExchangeSent)
	}

	// Received exchange.
	rcvdExch := ""
	if qso.RecvExchange != nil {
		rcvdExch = strings.TrimSpace(*qso.RecvExchange)
	}

	// Build the line with consistent column spacing.
	return fmt.Sprintf("%-6s %-2s %s %s %-13s %-20s %-13s %s",
		freq, mode, date, timeStr,
		myCallsign, sentExch,
		qso.Callsign, rcvdExch,
	)
}

// modeForCabrillo converts ADIF mode names to Cabrillo mode abbreviations.
func modeForCabrillo(mode string) string {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "CW":
		return "CW"
	case "SSB", "LSB", "USB", "AM", "FM":
		return "PH"
	case "RTTY", "RTTYM":
		return "RY"
	case "FT8", "FT4", "PSK31", "PSK63", "DIGI", "DATA", "OLIVIA", "MFSK", "JT65", "JT9":
		return "DG"
	default:
		return "PH"
	}
}

// bandToFrequency returns a representative frequency (kHz) for a band name.
func bandToFrequency(band string) string {
	m := map[string]string{
		"160m": "1825", "80m": "3525", "40m": "7025",
		"30m": "10125", "20m": "14025", "17m": "18075",
		"15m": "21025", "12m": "24895", "10m": "28025",
		"6m": "50125", "2m": "144200", "70cm": "432100",
	}
	if v, ok := m[strings.ToLower(band)]; ok {
		return v
	}
	return "14025"
}

// wrapSoapbox splits a soapbox string into lines of at most maxLen characters.
func wrapSoapbox(text string, maxLen int) []string {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > maxLen {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	lines = append(lines, line)
	return lines
}

// ─── ADIF builder for contest QSOs ────────────────────────────────────────

func buildContestADIF(header db.GetContestForExportRow, qsos []db.GetContestQSOsForExportRow) string {
	var b bytes.Buffer
	writer := adifpkg.NewWriterWithOptions(&b, adifpkg.WriterOptions{
		ProgramVersion: "0.1.0",
		IncludeHeader:  true,
	})
	_ = writer.WriteHeader(
		adifpkg.Field{Name: "CREATED_TIMESTAMP", Value: time.Now().UTC().Format(time.RFC3339)},
	)

	myCallsign := "NOCALL"
	if header.MyCallsign != nil && strings.TrimSpace(*header.MyCallsign) != "" {
		myCallsign = strings.ToUpper(strings.TrimSpace(*header.MyCallsign))
	}

	for _, qso := range qsos {
		rec := &adifpkg.Record{}
		rec.Set("CALL", qso.Callsign)
		rec.Set("BAND", qso.Band)
		rec.Set("MODE", qso.Mode)
		if qso.Submode != nil {
			rec.Set("SUBMODE", *qso.Submode)
		}
		adifpkg.CanonicalizeRecordMode(rec)
		rec.Set("QSO_DATE", qso.DatetimeOn.Time.UTC().Format("20060102"))
		rec.Set("TIME_ON", qso.DatetimeOn.Time.UTC().Format("150405"))
		if qso.RstSent != nil {
			rec.Set("RST_SENT", *qso.RstSent)
		}
		if qso.RstRcvd != nil {
			rec.Set("RST_RCVD", *qso.RstRcvd)
		}
		if qso.FrequencyHz != nil {
			mhz := float64(*qso.FrequencyHz) / 1_000_000.0
			rec.Set("FREQ", fmt.Sprintf("%.6f", mhz))
		}
		rec.Set("STATION_CALLSIGN", myCallsign)
		rec.Set("CONTEST_ID", header.ContestCode)
		if qso.SentExchange != nil {
			rec.Set("STX_STRING", *qso.SentExchange)
		}
		if qso.SentSerial != nil {
			rec.Set("STX", fmt.Sprintf("%d", *qso.SentSerial))
		}
		if qso.RecvExchange != nil {
			rec.Set("SRX_STRING", *qso.RecvExchange)
		}
		if qso.RecvSerial != nil {
			rec.Set("SRX", fmt.Sprintf("%d", *qso.RecvSerial))
		}
		_ = writer.WriteRecord(rec)
	}

	return b.String()
}

func parseContestUUIDForCabrillo(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "contestUUID")
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid contest session UUID")
	}
	return parsed, nil
}
