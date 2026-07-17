package callsign

import (
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
	// IFTFullDumpURL is the preferred CSV source for Mexico amateur callsigns.
	// Source landing page: https://www.ift.org.mx/transparencia/datos-abiertos
	IFTFullDumpURL = "https://www.ift.org.mx/sites/default/files/contenidogeneral/transparencia/datos-abiertos/radioaficionados.csv"

	iftSource = "ift"
)

// ParseIFTCSV downloads and parses an IFT callsign CSV.
func ParseIFTCSV(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "ift_parser: downloading csv", slog.String("url", url))

	if isExcelURL(url) {
		return nil, fmt.Errorf("IFT source is an Excel file (%s). Convert to CSV before import", filepath.Ext(url))
	}

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "ift_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseIFTCSVData(ctx, data)
}

// ParseIFTCSVData parses IFT CSV bytes already loaded in memory.
func ParseIFTCSVData(ctx context.Context, data []byte) (*ParseResult, error) {
	decoded := decodeIFTText(data)
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

	idx := iftHeaderIndex(header)
	if iftFindColumn(idx, "indicativo", "callsign", "call_sign", "distintivo") == -1 {
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
		norm := normalizeIFTRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "ift_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func normalizeIFTRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(iftField(row, idx,
		"indicativo", "callsign", "call_sign", "distintivo", "indicativo_de_llamada")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	fullName := strings.TrimSpace(iftField(row, idx,
		"nombre", "nombre_titular", "titular", "razon_social", "licensee", "nombre_del_titular"))

	state := strings.TrimSpace(iftField(row, idx,
		"estado", "entidad_federativa", "state", "state_province"))
	city := strings.TrimSpace(iftField(row, idx,
		"municipio", "delegacion", "demarcacion", "city", "localidad"))

	grantDate := parseIFTDate(iftField(row, idx,
		"fecha_otorgamiento", "fecha_expedicion", "fecha_emision", "grant_date"))
	expiryDate := parseIFTDate(iftField(row, idx,
		"fecha_vencimiento", "fecha_expiracion", "fecha_expira", "expiry_date"))

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        iftSource,
		SourceID:      iftField(row, idx, "id", "folio", "numero", "numero_licencia"),
		FullName:      fullName,
		City:          city,
		StateProvince: state,
		Country:       "Mexico",
		GrantDate:     grantDate,
		ExpiryDate:    expiryDate,
		Status:        "active",
	}
}

func iftField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeIFTHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func iftHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeIFTHeader(h)] = i
	}
	return idx
}

func iftFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeIFTHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

func canonicalizeIFTHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	replacer := strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ñ", "n", "ç", "c",
		"'", " ", "’", " ", "-", " ", ".", " ",
	)
	s = replacer.Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.Join(parts, "_")
}

func decodeIFTText(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
}

func parseIFTDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	layouts := []string{
		"2006-01-02",
		"02/01/2006",
		"02-01-2006",
		"02.01.2006",
		"2006/01/02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func isExcelURL(url string) bool {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(url)))
	return ext == ".xls" || ext == ".xlsx"
}

func looksLikeHTML(data []byte) bool {
	s := strings.ToLower(strings.TrimSpace(string(data[:min(512, len(data))])))
	return strings.Contains(s, "<html") || strings.Contains(s, "<!doctype html")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
