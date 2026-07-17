package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

const exportADIFSQL = `
SELECT
	q.id,
	q.callsign,
	q.band,
	q.mode,
	q.submode,
	q.frequency_hz,
	q.freq_rx_hz,
	q.datetime_on,
	q.datetime_off,
	q.rst_sent,
	q.rst_rcvd,
	q.name,
	q.qth,
	q.tx_power::text AS tx_power,
	q.rx_pwr::text AS rx_pwr,
	q.my_antenna,
	q.my_rig,
	q.gridsquare,
	q.dxcc,
	q.country,
	q.state,
	q.county,
	q.cq_zone,
	q.itu_zone,
	q.continent,
	q.my_gridsquare,
	q.my_city,
	q.my_state,
	q.my_country,
	q.my_dxcc,
	q.sfi,
	q.a_index,
	q.k_index,
	q.operator,
	q.station_callsign,
	q.contest_id,
	q.srx,
	q.stx,
	q.srx_string,
	q.stx_string,
	q.sat_name,
	q.sat_mode,
	q.prop_mode,
	q.sota_ref,
	q.my_sota_ref,
	q.pota_refs,
	q.my_pota_refs,
	q.wwff_ref,
	q.my_wwff_ref,
	q.iota,
	q.sig,
	q.sig_info,
	q.qsl_sent,
	to_char(q.qsl_sent_date, 'YYYYMMDD') AS qslsdate,
	q.qsl_rcvd,
	to_char(q.qsl_rcvd_date, 'YYYYMMDD') AS qslrdate,
	q.qsl_via,
	q.lotw_qsl_sent,
	to_char(q.lotw_qsl_sent_date, 'YYYYMMDD') AS lotw_qslsdate,
	q.lotw_qsl_rcvd,
	to_char(q.lotw_qsl_rcvd_date, 'YYYYMMDD') AS lotw_qslrdate,
	q.eqsl_qsl_sent,
	to_char(q.eqsl_qsl_sent_date, 'YYYYMMDD') AS eqsl_qslsdate,
	q.eqsl_qsl_rcvd,
	to_char(q.eqsl_qsl_rcvd_date, 'YYYYMMDD') AS eqsl_qslrdate,
	q.comment,
	q.notes,
	q.extra
FROM qsos q
JOIN logbooks l ON l.id = q.logbook_id
WHERE l.deleted_at IS NULL
	AND q.deleted_at IS NULL
	AND ($1::uuid IS NULL OR l.uuid = $1::uuid)
	AND ($2::text IS NULL OR LOWER(q.band) = LOWER($2::text))
	AND ($3::text IS NULL OR UPPER(q.mode) = UPPER($3::text))
	AND ($4::integer IS NULL OR q.dxcc = $4::integer)
	AND ($5::timestamptz IS NULL OR q.datetime_on >= $5::timestamptz)
	AND ($6::timestamptz IS NULL OR q.datetime_on <= $6::timestamptz)
ORDER BY q.datetime_on ASC, q.id ASC
`

// ExportHandler handles ADIF export endpoints.
type ExportHandler struct {
	pool *pgxpool.Pool
}

// NewExportHandler creates an ExportHandler with dependencies.
func NewExportHandler(pool *pgxpool.Pool) *ExportHandler {
	return &ExportHandler{pool: pool}
}

// ADIF handles GET /v1/export/adif.
func (h *ExportHandler) ADIF(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	filters, err := parseExportFilters(r)
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
	if filters.LogbookUUID != nil {
		roleQueries := db.New(tx)
		if _, err := ensureLogbookPermission(r.Context(), roleQueries, userID, *filters.LogbookUUID, auth.PermissionExportADIF); err != nil {
			if errors.Is(err, errForbiddenRBAC) {
				writeFailure(w, http.StatusForbidden, "forbidden", "insufficient permissions")
				return
			}
			writeFailure(w, http.StatusInternalServerError, "authorization failed", "could not resolve logbook role")
			return
		}
	}

	rows, err := tx.Query(
		r.Context(),
		exportADIFSQL,
		filters.LogbookUUID,
		filters.Band,
		filters.Mode,
		filters.Dxcc,
		filters.DateFrom,
		filters.DateTo,
	)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "failed to export adif", "query failed")
		return
	}
	defer rows.Close()

	filename := buildADIFFilename(filters.LogbookUUID)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store")

	writer := adifpkg.NewWriterWithOptions(w, adifpkg.WriterOptions{
		ProgramVersion: "0.1.0",
		IncludeHeader:  true,
	})
	if err := writer.WriteHeader(); err != nil {
		return
	}

	flusher, canFlush := w.(http.Flusher)
	count := 0

	for rows.Next() {
		row, scanErr := scanExportRow(rows)
		if scanErr != nil {
			return
		}

		rec := row.toADIFRecord()
		if err := writer.WriteRecord(rec); err != nil {
			return
		}

		count++
		if canFlush && count%100 == 0 {
			flusher.Flush()
		}
	}

	if rows.Err() != nil {
		return
	}

	if canFlush {
		flusher.Flush()
	}
}

type exportFilters struct {
	LogbookUUID *uuid.UUID
	Band        *string
	Mode        *string
	Dxcc        *int32
	DateFrom    *time.Time
	DateTo      *time.Time
}

func parseExportFilters(r *http.Request) (exportFilters, error) {
	q := r.URL.Query()
	var f exportFilters

	if raw := strings.TrimSpace(q.Get("logbook_uuid")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return exportFilters{}, fmt.Errorf("invalid logbook_uuid")
		}
		f.LogbookUUID = &parsed
	}

	if raw := strings.TrimSpace(q.Get("band")); raw != "" {
		f.Band = &raw
	}

	if raw := strings.TrimSpace(q.Get("mode")); raw != "" {
		f.Mode = &raw
	}

	if raw := strings.TrimSpace(q.Get("dxcc")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return exportFilters{}, fmt.Errorf("invalid dxcc")
		}
		n32 := int32(n)
		f.Dxcc = &n32
	}

	if raw := strings.TrimSpace(q.Get("date_from")); raw != "" {
		parsed, err := parseExportDate(raw, false)
		if err != nil {
			return exportFilters{}, fmt.Errorf("invalid date_from")
		}
		f.DateFrom = &parsed
	}

	if raw := strings.TrimSpace(q.Get("date_to")); raw != "" {
		parsed, err := parseExportDate(raw, true)
		if err != nil {
			return exportFilters{}, fmt.Errorf("invalid date_to")
		}
		f.DateTo = &parsed
	}

	if f.DateFrom != nil && f.DateTo != nil && f.DateFrom.After(*f.DateTo) {
		return exportFilters{}, fmt.Errorf("date_from must be before or equal to date_to")
	}

	return f, nil
}

func parseExportDate(raw string, endOfDay bool) (time.Time, error) {
	layouts := []string{time.RFC3339, "2006-01-02", "20060102"}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		parsed = parsed.UTC()
		if layout == "2006-01-02" || layout == "20060102" {
			if endOfDay {
				parsed = parsed.Add(24*time.Hour - time.Nanosecond)
			}
		}
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("invalid date")
}

func buildADIFFilename(logbookUUID *uuid.UUID) string {
	ts := time.Now().UTC().Format("20060102-150405")
	if logbookUUID != nil {
		prefix := strings.Split(logbookUUID.String(), "-")[0]
		return fmt.Sprintf("radioledger-export-%s-%s.adi", prefix, ts)
	}
	return fmt.Sprintf("radioledger-export-%s.adi", ts)
}

type exportQSO struct {
	ID              int64
	Callsign        string
	Band            string
	Mode            string
	Submode         *string
	FrequencyHz     *int64
	FreqRxHz        *int64
	DatetimeOn      time.Time
	DatetimeOff     *time.Time
	RstSent         *string
	RstRcvd         *string
	Name            *string
	QTH             *string
	TxPower         *string
	RxPwr           *string
	MyAntenna       *string
	MyRig           *string
	Gridsquare      *string
	Dxcc            *int32
	Country         *string
	State           *string
	County          *string
	CqZone          *int16
	ItuZone         *int16
	Continent       *string
	MyGridsquare    *string
	MyCity          *string
	MyState         *string
	MyCountry       *string
	MyDxcc          *int32
	Sfi             *int16
	AIndex          *int16
	KIndex          *int16
	Operator        *string
	StationCallsign *string
	ContestID       *string
	Srx             *string
	Stx             *string
	SrxString       *string
	StxString       *string
	SatName         *string
	SatMode         *string
	PropMode        *string
	SotaRef         *string
	MySotaRef       *string
	PotaRefs        []string
	MyPotaRefs      []string
	WwffRef         *string
	MyWwffRef       *string
	Iota            *string
	Sig             *string
	SigInfo         *string
	QslSent         *string
	QslSentDate     *string
	QslRcvd         *string
	QslRcvdDate     *string
	QslVia          *string
	LotwQslSent     *string
	LotwQslSentDate *string
	LotwQslRcvd     *string
	LotwQslRcvdDate *string
	EqslQslSent     *string
	EqslQslSentDate *string
	EqslQslRcvd     *string
	EqslQslRcvdDate *string
	Comment         *string
	Notes           *string
	Extra           []byte
}

func scanExportRow(rows pgx.Rows) (exportQSO, error) {
	var row exportQSO
	err := rows.Scan(
		&row.ID,
		&row.Callsign,
		&row.Band,
		&row.Mode,
		&row.Submode,
		&row.FrequencyHz,
		&row.FreqRxHz,
		&row.DatetimeOn,
		&row.DatetimeOff,
		&row.RstSent,
		&row.RstRcvd,
		&row.Name,
		&row.QTH,
		&row.TxPower,
		&row.RxPwr,
		&row.MyAntenna,
		&row.MyRig,
		&row.Gridsquare,
		&row.Dxcc,
		&row.Country,
		&row.State,
		&row.County,
		&row.CqZone,
		&row.ItuZone,
		&row.Continent,
		&row.MyGridsquare,
		&row.MyCity,
		&row.MyState,
		&row.MyCountry,
		&row.MyDxcc,
		&row.Sfi,
		&row.AIndex,
		&row.KIndex,
		&row.Operator,
		&row.StationCallsign,
		&row.ContestID,
		&row.Srx,
		&row.Stx,
		&row.SrxString,
		&row.StxString,
		&row.SatName,
		&row.SatMode,
		&row.PropMode,
		&row.SotaRef,
		&row.MySotaRef,
		&row.PotaRefs,
		&row.MyPotaRefs,
		&row.WwffRef,
		&row.MyWwffRef,
		&row.Iota,
		&row.Sig,
		&row.SigInfo,
		&row.QslSent,
		&row.QslSentDate,
		&row.QslRcvd,
		&row.QslRcvdDate,
		&row.QslVia,
		&row.LotwQslSent,
		&row.LotwQslSentDate,
		&row.LotwQslRcvd,
		&row.LotwQslRcvdDate,
		&row.EqslQslSent,
		&row.EqslQslSentDate,
		&row.EqslQslRcvd,
		&row.EqslQslRcvdDate,
		&row.Comment,
		&row.Notes,
		&row.Extra,
	)
	return row, err
}

func (q exportQSO) toADIFRecord() *adifpkg.Record {
	rec := &adifpkg.Record{}

	setField(rec, "CALL", q.Callsign)
	setField(rec, "BAND", q.Band)
	setField(rec, "MODE", q.Mode)
	setFieldPtr(rec, "SUBMODE", q.Submode)
	adifpkg.CanonicalizeRecordMode(rec)

	on := q.DatetimeOn.UTC()
	setField(rec, "QSO_DATE", on.Format("20060102"))
	setField(rec, "TIME_ON", on.Format("150405"))
	if q.DatetimeOff != nil {
		off := q.DatetimeOff.UTC()
		setField(rec, "QSO_DATE_OFF", off.Format("20060102"))
		setField(rec, "TIME_OFF", off.Format("150405"))
	}

	setFreqField(rec, "FREQ", q.FrequencyHz)
	setFreqField(rec, "FREQ_RX", q.FreqRxHz)

	setFieldPtr(rec, "RST_SENT", q.RstSent)
	setFieldPtr(rec, "RST_RCVD", q.RstRcvd)
	setFieldPtr(rec, "NAME", q.Name)
	setFieldPtr(rec, "QTH", q.QTH)
	setFieldPtr(rec, "TX_PWR", q.TxPower)
	setFieldPtr(rec, "RX_PWR", q.RxPwr)
	setFieldPtr(rec, "MY_ANTENNA", q.MyAntenna)
	setFieldPtr(rec, "MY_RIG", q.MyRig)
	setFieldPtr(rec, "GRIDSQUARE", q.Gridsquare)
	setFieldPtr(rec, "COUNTRY", q.Country)
	setFieldPtr(rec, "STATE", q.State)
	setFieldPtr(rec, "COUNTY", q.County)
	setFieldPtr(rec, "CONT", q.Continent)
	setFieldPtr(rec, "MY_GRIDSQUARE", q.MyGridsquare)
	setFieldPtr(rec, "MY_CITY", q.MyCity)
	setFieldPtr(rec, "MY_STATE", q.MyState)
	setFieldPtr(rec, "MY_COUNTRY", q.MyCountry)
	setFieldPtr(rec, "OPERATOR", q.Operator)
	setFieldPtr(rec, "STATION_CALLSIGN", q.StationCallsign)
	setFieldPtr(rec, "CONTEST_ID", q.ContestID)
	setFieldPtr(rec, "SRX", q.Srx)
	setFieldPtr(rec, "STX", q.Stx)
	setFieldPtr(rec, "SRX_STRING", q.SrxString)
	setFieldPtr(rec, "STX_STRING", q.StxString)
	setFieldPtr(rec, "SAT_NAME", q.SatName)
	setFieldPtr(rec, "SAT_MODE", q.SatMode)
	setFieldPtr(rec, "PROP_MODE", q.PropMode)
	setFieldPtr(rec, "SOTA_REF", q.SotaRef)
	setFieldPtr(rec, "MY_SOTA_REF", q.MySotaRef)
	setFieldPtr(rec, "WWFF_REF", q.WwffRef)
	setFieldPtr(rec, "MY_WWFF_REF", q.MyWwffRef)
	setFieldPtr(rec, "IOTA", q.Iota)
	setFieldPtr(rec, "SIG", q.Sig)
	setFieldPtr(rec, "SIG_INFO", q.SigInfo)
	setFieldPtr(rec, "QSL_SENT", q.QslSent)
	setFieldPtr(rec, "QSLSDATE", q.QslSentDate)
	setFieldPtr(rec, "QSL_RCVD", q.QslRcvd)
	setFieldPtr(rec, "QSLRDATE", q.QslRcvdDate)
	setFieldPtr(rec, "QSL_VIA", q.QslVia)
	setFieldPtr(rec, "LOTW_QSL_SENT", q.LotwQslSent)
	setFieldPtr(rec, "LOTW_QSLSDATE", q.LotwQslSentDate)
	setFieldPtr(rec, "LOTW_QSL_RCVD", q.LotwQslRcvd)
	setFieldPtr(rec, "LOTW_QSLRDATE", q.LotwQslRcvdDate)
	setFieldPtr(rec, "EQSL_QSL_SENT", q.EqslQslSent)
	setFieldPtr(rec, "EQSL_QSLSDATE", q.EqslQslSentDate)
	setFieldPtr(rec, "EQSL_QSL_RCVD", q.EqslQslRcvd)
	setFieldPtr(rec, "EQSL_QSLRDATE", q.EqslQslRcvdDate)
	setFieldPtr(rec, "COMMENT", q.Comment)
	setFieldPtr(rec, "NOTES", q.Notes)

	setFieldInt32(rec, "DXCC", q.Dxcc)
	setFieldInt16(rec, "CQZ", q.CqZone)
	setFieldInt16(rec, "ITUZ", q.ItuZone)
	setFieldInt32(rec, "MY_DXCC", q.MyDxcc)
	setFieldInt16(rec, "SFI", q.Sfi)
	setFieldInt16(rec, "A_INDEX", q.AIndex)
	setFieldInt16(rec, "K_INDEX", q.KIndex)

	if len(q.PotaRefs) > 0 {
		setField(rec, "POTA_REF", strings.Join(q.PotaRefs, ","))
	}
	if len(q.MyPotaRefs) > 0 {
		setField(rec, "MY_POTA_REF", strings.Join(q.MyPotaRefs, ","))
	}

	mergeExtraFields(rec, q.Extra)
	return rec
}

func setField(rec *adifpkg.Record, name, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return
	}
	rec.Set(name, trimmed)
}

func setFieldPtr(rec *adifpkg.Record, name string, value *string) {
	if value == nil {
		return
	}
	setField(rec, name, *value)
}

func setFieldInt32(rec *adifpkg.Record, name string, value *int32) {
	if value == nil {
		return
	}
	rec.Set(name, strconv.FormatInt(int64(*value), 10))
}

func setFieldInt16(rec *adifpkg.Record, name string, value *int16) {
	if value == nil {
		return
	}
	rec.Set(name, strconv.FormatInt(int64(*value), 10))
}

func setFreqField(rec *adifpkg.Record, name string, hz *int64) {
	if hz == nil || *hz <= 0 {
		return
	}
	rec.Set(name, formatHzAsMHz(*hz))
}

func formatHzAsMHz(hz int64) string {
	mhz := float64(hz) / 1_000_000
	s := strconv.FormatFloat(mhz, 'f', 6, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

func mergeExtraFields(rec *adifpkg.Record, raw []byte) {
	if len(raw) == 0 {
		return
	}

	extra := map[string]any{}
	if err := json.Unmarshal(raw, &extra); err != nil {
		return
	}

	for key, value := range extra {
		name := strings.ToUpper(strings.TrimSpace(key))
		if name == "" || rec.Has(name) {
			continue
		}

		stringValue := stringifyExtraValue(value)
		if strings.TrimSpace(stringValue) == "" {
			continue
		}

		rec.Set(name, stringValue)
	}
}

func stringifyExtraValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
