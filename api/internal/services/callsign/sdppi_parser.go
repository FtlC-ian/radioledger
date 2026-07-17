package callsign

// Indonesia SDPPI amateur radio callsign parser.
//
// # Data Source
//
// SDPPI (Sumber Daya dan Perangkat Pos dan Informatika) is Indonesia's telecom
// regulator.  Indonesian amateur callsigns use the ITU prefixes YB–YH.
//
// The official public lookup portal (https://iar-ikrap.postel.go.id) supports
// single-callsign queries only — the endpoint at /registrant/searchDataIar/
// accepts a ?callsign= parameter and returns an HTML fragment.  There is no
// bulk CSV or API export available.  The older SIARAS system
// (siaras.postel.go.id) no longer resolves DNS.
//
// # Manual import workflow
//
// Until SDPPI publishes a bulk data feed, operators can supply a CSV exported
// from a community-maintained dataset (e.g. from ORARI, the Indonesian
// national amateur radio organisation) or generated via the single-callsign
// API.  The expected columns are documented on ParseSdppiCSVData.
//
// License classes used by SDPPI / ORARI:
//
//	SIAGA    (Novice)  — entry level
//	PENGGALANG (General) — intermediate
//	PENEGAK  (Extra Class / Advanced) — full privileges

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	sdppiSource = "sdppi"
)

// ParseSdppiCSVFile reads a CSV file from disk and parses it.
func ParseSdppiCSVFile(ctx context.Context, path string) (*ParseResult, error) {
	slog.InfoContext(ctx, "sdppi_parser: reading file", slog.String("path", path))

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	slog.InfoContext(ctx, "sdppi_parser: file read",
		slog.String("path", path),
		slog.Int("bytes", len(data)),
	)

	return ParseSdppiCSVData(ctx, data)
}

// ParseSdppiCSVData parses SDPPI callsign CSV bytes already loaded in memory.
//
// The parser accepts comma- or semicolon-delimited files in UTF-8 or ISO-8859-1
// encoding (Indonesian names are ASCII-safe but some community exports use
// Latin-1).  A flexible set of column-name aliases is recognised so the parser
// handles exports from multiple community sources.
//
// Recognised column names (case-insensitive, diacritics stripped):
//
//	callsign / tanda_panggilan / indicatif / call_sign
//	full_name / nama / nama_pemilik / nama_lengkap / owner
//	province / provinsi / state / state_province
//	city / kota / kabupaten / kabupaten_kota / municipality
//	license_class / tingkat / kelas / class
//	grant_date / tanggal_terbit / issued / issue_date / start_date
//	expiry_date / masa_laku / expired / expire_date / end_date
//	status / aktif
//	source_id / no_izin / nomor_izin / license_number / id
func ParseSdppiCSVData(ctx context.Context, data []byte) (*ParseResult, error) {
	decoded := decodeSdppiText(data)
	if looksLikeHTML(decoded) {
		return nil, fmt.Errorf("payload looks like HTML, not CSV")
	}

	r := csv.NewReader(bytes.NewReader(decoded))
	r.Comma = detectCSVDelimiter(decoded)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	idx := sdppiHeaderIndex(header)
	if sdppiFindColumn(idx, "tanda_panggilan", "callsign", "call_sign", "indicatif") == -1 {
		return nil, fmt.Errorf("missing required callsign column (got: %v)", header)
	}

	result := &ParseResult{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			slog.WarnContext(ctx, "sdppi_parser: skipping malformed row", slog.String("error", err.Error()))
			result.Skipped++
			continue
		}

		result.Processed++
		norm := normalizeSdppiRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "sdppi_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

// normalizeSdppiRow converts a single CSV row into a NormalizedRecord.
func normalizeSdppiRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(sdppiField(row, idx,
		"tanda_panggilan", "callsign", "call_sign", "indicatif")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	fullName := strings.TrimSpace(sdppiField(row, idx,
		"nama_pemilik", "nama", "nama_lengkap", "full_name", "owner"))

	province := strings.TrimSpace(sdppiField(row, idx,
		"provinsi", "province", "state", "state_province"))

	city := strings.TrimSpace(sdppiField(row, idx,
		"kota", "kabupaten_kota", "kabupaten", "city", "municipality"))

	licenseClass := normalizeSdppiLicenseClass(sdppiField(row, idx,
		"tingkat", "kelas", "license_class", "class"))

	sourceID := sdppiField(row, idx,
		"no_izin", "nomor_izin", "license_number", "source_id", "id")

	grantDate := parseSdppiDate(sdppiField(row, idx,
		"tanggal_terbit", "issued", "issue_date", "start_date", "grant_date"))

	expiryDate := parseSdppiDate(sdppiField(row, idx,
		"masa_laku", "expired", "expire_date", "end_date", "expiry_date"))

	statusRaw := sdppiField(row, idx, "status", "aktif")

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        sdppiSource,
		SourceID:      sourceID,
		FullName:      fullName,
		StateProvince: province,
		City:          city,
		Country:       "Indonesia",
		LicenseClass:  licenseClass,
		GrantDate:     grantDate,
		ExpiryDate:    expiryDate,
		Status:        normalizeSdppiStatus(statusRaw),
	}
}

// normalizeSdppiLicenseClass maps SDPPI/ORARI Indonesian license class labels
// to lowercase underscore-separated tokens consistent with other parsers.
//
// SDPPI / ORARI use three amateur radio license levels:
//
//	SIAGA     (Novice)       — entry level, restricted privileges
//	PENGGALANG (General)    — intermediate privileges
//	PENEGAK   (Extra Class) — full privileges (also called "Tingkat Tinggi")
func normalizeSdppiLicenseClass(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	switch {
	case strings.Contains(s, "penegak") || strings.Contains(s, "extra") || strings.Contains(s, "tinggi"):
		return "extra"
	case strings.Contains(s, "penggalang") || strings.Contains(s, "general") || strings.Contains(s, "menengah"):
		return "general"
	case strings.Contains(s, "siaga") || strings.Contains(s, "novice") || strings.Contains(s, "pemula"):
		return "novice"
	default:
		if s == "" {
			return ""
		}
		// Preserve unrecognised values as-is (cleaned).
		parts := strings.FieldsFunc(s, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
		})
		return strings.Join(parts, "_")
	}
}

// normalizeSdppiStatus maps Indonesian status strings to "active" or "expired".
func normalizeSdppiStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tidak aktif", "tidak_aktif", "expired", "mati", "hangus", "dicabut", "revoked",
		"batal", "dibatalkan", "cancelled", "canceled":
		return "expired"
	default:
		// "aktif", "active", "1", "true", or empty → assume active.
		return "active"
	}
}

// parseSdppiDate parses common date formats found in Indonesian government exports.
func parseSdppiDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	// Normalise written Indonesian month names (e.g. "NOPEMBER 2028" → "NOVEMBER 2028").
	s = strings.NewReplacer(
		"JANUARI", "January",
		"FEBRUARI", "February",
		"MARET", "March",
		"APRIL", "April",
		"MEI", "May",
		"JUNI", "June",
		"JULI", "July",
		"AGUSTUS", "August",
		"SEPTEMBER", "September",
		"OKTOBER", "October",
		"NOPEMBER", "November",
		"NOVEMBER", "November",
		"DESEMBER", "December",
		"JAN", "January",
		"FEB", "February",
		"MAR", "March",
		"APR", "April",
		"MEI", "May",
		"JUN", "June",
		"JUL", "July",
		"AGT", "August",
		"AGU", "August",
		"SEP", "September",
		"OKT", "October",
		"NOP", "November",
		"NOV", "November",
		"DES", "December",
	).Replace(strings.ToUpper(s))

	layouts := []string{
		"2006-01-02",
		"02/01/2006",
		"02-01-2006",
		"02.01.2006",
		"2006/01/02",
		"01/02/2006",
		"January 2006",  // e.g. "November 2028" (month/year only, day=1)
		"Jan 2006",
		"2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// ── Header helpers ────────────────────────────────────────────────────────────

func sdppiField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeSdppiHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func sdppiHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeSdppiHeader(h)] = i
	}
	return idx
}

func sdppiFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeSdppiHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

// canonicalizeSdppiHeader lower-cases an Indonesian column name, strips
// diacritics, and collapses punctuation/spaces to underscores for fuzzy
// matching.
func canonicalizeSdppiHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
		"'", " ", "'", " ", "-", " ", ".", " ",
	).Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.Join(parts, "_")
}

// decodeSdppiText converts ISO-8859-1 / Latin-1 encoded SDPPI data to UTF-8,
// or returns the input unchanged if it is already valid UTF-8.
func decodeSdppiText(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // strip BOM
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
}
