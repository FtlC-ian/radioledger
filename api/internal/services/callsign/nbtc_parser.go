package callsign

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	// NBTCFullDumpURL is the Thailand NBTC amateur radio dataset source.
	// TODO(issue-106): replace this placeholder with the exact CSV export URL from
	// data.go.th once the NBTC dataset resource URL is confirmed.
	NBTCFullDumpURL = "https://data.go.th/dataset/nbtc-amateur-radio-callsigns/resource/nbtc-amateur-radio-callsigns.csv"

	nbtcSource = "nbtc"
)

// ParseNBTC downloads and parses the NBTC amateur radio dataset.
// The source is expected to be CSV, but zip-wrapped CSV is also supported.
func ParseNBTC(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "nbtc_parser: downloading dataset", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "nbtc_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseNBTCData(ctx, data)
}

// ParseNBTCData parses an NBTC dataset already loaded in memory.
func ParseNBTCData(ctx context.Context, data []byte) (*ParseResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty dataset")
	}

	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return parseNBTCZipData(ctx, data)
	}

	decoded := decodeNBTCText(data)
	if looksLikeHTML(decoded) {
		return nil, fmt.Errorf("downloaded payload looks like HTML, not CSV")
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

	idx := nbtcHeaderIndex(header)
	if nbtcFindColumn(idx,
		"callsign", "call_sign", "call sign",
		"สัญญาณเรียกขาน", "เครื่องหมายเรียกขาน", "ชื่อเรียกขาน",
	) == -1 {
		return nil, fmt.Errorf("missing required callsign column")
	}

	result := &ParseResult{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		result.Processed++
		norm := normalizeNBTCRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "nbtc_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func parseNBTCZipData(ctx context.Context, data []byte) (*ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	var candidate *zip.File
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if strings.Contains(name, "readme") || strings.Contains(name, "license") {
			continue
		}
		if strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".txt") {
			if candidate == nil || f.UncompressedSize64 > candidate.UncompressedSize64 {
				candidate = f
			}
		}
	}
	if candidate == nil {
		return nil, fmt.Errorf("no CSV file found in NBTC zip")
	}

	rc, err := candidate.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", candidate.Name, err)
	}
	defer func() { _ = rc.Close() }()

	payload, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", candidate.Name, err)
	}

	return ParseNBTCData(ctx, payload)
}

func normalizeNBTCRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(nbtcField(row, idx,
		"callsign", "call_sign", "call sign",
		"สัญญาณเรียกขาน", "เครื่องหมายเรียกขาน", "ชื่อเรียกขาน",
	)))
	if !isNBTCCallsign(callsign) {
		return nil
	}

	firstName := strings.TrimSpace(nbtcField(row, idx,
		"first_name", "given_name", "ชื่อ", "ชื่อจริง"))
	lastName := strings.TrimSpace(nbtcField(row, idx,
		"last_name", "surname", "family_name", "นามสกุล", "ชื่อสกุล"))
	fullName := strings.TrimSpace(nbtcField(row, idx,
		"full_name", "name", "licensee", "licensee_name",
		"ชื่อผู้รับอนุญาต", "ชื่อ นามสกุล", "ชื่อ-นามสกุล", "ชื่อนามสกุล",
	))
	if fullName == "" {
		fullName = strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
	}

	status := normalizeNBTCStatus(nbtcField(row, idx, "status", "สถานะ"))
	if status == "" {
		status = "active"
	}

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        nbtcSource,
		SourceID:      strings.TrimSpace(nbtcField(row, idx, "id", "record_id", "license_id", "license_no", "license_number", "เลขที่ใบอนุญาต", "เลขที่ใบอนุญาตวิทยุคมนาคม")),
		FirstName:     firstName,
		LastName:      lastName,
		FullName:      fullName,
		City:          strings.TrimSpace(nbtcField(row, idx, "district", "amphoe", "city", "เขต", "อำเภอ", "อําเภอ", "เมือง")),
		StateProvince: strings.TrimSpace(nbtcField(row, idx, "province", "state", "state_province", "จังหวัด")),
		PostalCode:    strings.TrimSpace(nbtcField(row, idx, "postal_code", "zip", "zipcode", "รหัสไปรษณีย์")),
		Country:       "Thailand",
		LicenseClass:  normalizeNBTCLicenseClass(nbtcField(row, idx, "license_class", "class", "class_name", "ประเภท", "ประเภทใบอนุญาต", "ระดับ")),
		GrantDate:     parseNBTCDate(nbtcField(row, idx, "grant_date", "issue_date", "issued_date", "date_issued", "วันที่ออกใบอนุญาต", "วันที่อนุญาต")),
		ExpiryDate:    parseNBTCDate(nbtcField(row, idx, "expiry_date", "expire_date", "expiration_date", "วันที่หมดอายุ", "วันหมดอายุ", "วันสิ้นอายุ")),
		Status:        status,
	}
}

func isNBTCCallsign(callsign string) bool {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if !isLikelyCallsign(callsign) {
		return false
	}
	return strings.HasPrefix(callsign, "HS") || strings.HasPrefix(callsign, "E2")
}

func normalizeNBTCStatus(s string) string {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if normalized == "" {
		return ""
	}
	if strings.Contains(normalized, "หมดอายุ") || strings.Contains(normalized, "expired") {
		return "expired"
	}
	if strings.Contains(normalized, "ยกเลิก") || strings.Contains(normalized, "cancel") || strings.Contains(normalized, "revoked") {
		return "cancelled"
	}
	if strings.Contains(normalized, "active") || strings.Contains(normalized, "issued") || strings.Contains(normalized, "valid") || strings.Contains(normalized, "ใช้งาน") || strings.Contains(normalized, "มีผล") {
		return "active"
	}
	return "active"
}

func normalizeNBTCLicenseClass(s string) string {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if normalized == "" {
		return ""
	}
	switch normalized {
	case "ขั้นต้น":
		return "foundation"
	case "ขั้นกลาง":
		return "intermediate"
	case "ขั้นสูง":
		return "advanced"
	}
	normalized = strings.ReplaceAll(normalized, "-", " ")
	normalized = strings.ReplaceAll(normalized, "/", " ")
	return strings.Join(strings.Fields(normalized), "_")
}

func parseNBTCDate(s string) *time.Time {
	s = strings.TrimSpace(strings.TrimPrefix(s, "\ufeff"))
	if s == "" {
		return nil
	}

	for _, layout := range []string{
		"2006-01-02",
		"02/01/2006",
		"2/1/2006",
		"02-01-2006",
		"2-1-2006",
		"2006/01/02",
		"02.01.2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return normalizeNBTCTime(t)
		}
	}
	return nil
}

func normalizeNBTCTime(t time.Time) *time.Time {
	year := t.Year()
	if year > 2400 {
		year -= 543
	}
	normalized := time.Date(year, t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &normalized
}

func nbtcField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeNBTCHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func nbtcHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeNBTCHeader(h)] = i
	}
	return idx
}

func nbtcFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeNBTCHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

func canonicalizeNBTCHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(strings.TrimPrefix(s, "\ufeff")))
	replacer := strings.NewReplacer(
		"'", " ", "’", " ", "-", " ", ".", " ", "(", " ", ")", " ",
	)
	s = replacer.Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.Join(parts, "_")
}

func decodeNBTCText(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	}
	decoded, err := charmap.Windows874.NewDecoder().Bytes(data)
	if err == nil && utf8.Valid(decoded) {
		return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
	}
	decoded, err = charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
}
