package callsign

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	// RDIFullDumpURL is the RDI Dutch amateur callsign open-data source.
	// Source landing page: https://www.rdi.nl/documenten/open-data
	RDIFullDumpURL = "https://www.rdi.nl/documenten/open-data"

	rdiSource = "rdi"
)

// ParseRDI downloads and parses an RDI amateur callsign dataset.
// The source can be CSV, JSON, or a zip containing either format.
func ParseRDI(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "rdi_parser: downloading dataset", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "rdi_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseRDIData(ctx, data)
}

// ParseRDIData parses RDI dataset bytes already loaded in memory.
func ParseRDIData(ctx context.Context, data []byte) (*ParseResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty dataset")
	}

	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return parseRDIZipData(ctx, data)
	}

	decoded := decodeRDIText(data)
	if looksLikeHTML(decoded) {
		return nil, fmt.Errorf("downloaded payload looks like HTML, not CSV/JSON")
	}
	if looksLikeJSON(decoded) {
		return parseRDIJSONData(ctx, decoded)
	}
	return parseRDICSVData(ctx, decoded)
}

func parseRDIZipData(ctx context.Context, data []byte) (*ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	var fallback *zip.File
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if strings.Contains(name, "readme") || strings.Contains(name, "license") {
			continue
		}
		if strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".txt") {
			if fallback == nil || f.UncompressedSize64 > fallback.UncompressedSize64 {
				fallback = f
			}
		}
	}
	if fallback == nil {
		return nil, fmt.Errorf("no CSV/JSON file found in RDI zip")
	}

	rc, err := fallback.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", fallback.Name, err)
	}
	defer func() { _ = rc.Close() }()

	payload, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", fallback.Name, err)
	}

	return ParseRDIData(ctx, payload)
}

func parseRDICSVData(ctx context.Context, data []byte) (*ParseResult, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = detectCSVDelimiter(data)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	idx := rdiHeaderIndex(header)
	if rdiFindColumn(idx, "roepnaam", "callsign", "call_sign", "call") == -1 {
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
		norm := normalizeRDIRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "rdi_parser: parsed csv rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func parseRDIJSONData(ctx context.Context, data []byte) (*ParseResult, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	objects := extractRDIObjects(raw)
	if len(objects) == 0 {
		return nil, fmt.Errorf("no records found in JSON payload")
	}

	result := &ParseResult{}
	for _, obj := range objects {
		result.Processed++
		norm := normalizeRDIObject(obj)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "rdi_parser: parsed json rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func normalizeRDIRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(rdiField(row, idx,
		"roepnaam", "callsign", "call_sign", "call")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	return &NormalizedRecord{
		Callsign:     callsign,
		Source:       rdiSource,
		SourceID:     rdiField(row, idx, "id", "nummer", "registratie_nummer", "registratienummer", "reference"),
		FullName:     rdiField(row, idx, "naam", "name", "houder", "houdernaam", "licensee"),
		City:         rdiField(row, idx, "plaatsnaam", "woonplaats", "city", "plaats", "gemeente"),
		Country:      "Netherlands",
		LicenseClass: normalizeRDILicenseClass(rdiField(row, idx, "vergunningtype", "vergunning_type", "license_type", "licensetype", "registratie_type", "machtigingsklasse", "klasse", "class")),
		Status:       normalizeRDIStatus(rdiField(row, idx, "status", "state")),
	}
}

func normalizeRDIObject(obj map[string]any) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(rdiJSONField(obj,
		"roepnaam", "callsign", "call_sign", "call")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	return &NormalizedRecord{
		Callsign:     callsign,
		Source:       rdiSource,
		SourceID:     rdiJSONField(obj, "id", "nummer", "registratie_nummer", "registratienummer", "reference"),
		FullName:     rdiJSONField(obj, "naam", "name", "houder", "houdernaam", "licensee"),
		City:         rdiJSONField(obj, "plaatsnaam", "woonplaats", "city", "plaats", "gemeente"),
		Country:      "Netherlands",
		LicenseClass: normalizeRDILicenseClass(rdiJSONField(obj, "vergunningtype", "vergunning_type", "license_type", "licensetype", "registratie_type", "machtigingsklasse", "klasse", "class")),
		Status:       normalizeRDIStatus(rdiJSONField(obj, "status", "state")),
	}
}

func extractRDIObjects(raw any) []map[string]any {
	switch v := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"records", "items", "results", "data", "callsigns", "value"} {
			if nested, ok := v[key]; ok {
				if objs := extractRDIObjects(nested); len(objs) > 0 {
					return objs
				}
			}
		}
		return []map[string]any{v}
	default:
		return nil
	}
}

func rdiField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeRDIHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func rdiJSONField(obj map[string]any, aliases ...string) string {
	index := make(map[string]string, len(obj))
	for k, v := range obj {
		index[canonicalizeRDIHeader(k)] = toJSONString(v)
	}
	for _, alias := range aliases {
		if v, ok := index[canonicalizeRDIHeader(alias)]; ok {
			return v
		}
	}
	return ""
}

func rdiHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeRDIHeader(h)] = i
	}
	return idx
}

func rdiFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeRDIHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

func canonicalizeRDIHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	replacer := strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n", "ß", "ss",
		"'", " ", "’", " ", "-", " ", ".", " ", "(", " ", ")", " ",
	)
	s = replacer.Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.Join(parts, "_")
}

func decodeRDIText(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
}

func looksLikeJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

func normalizeRDILicenseClass(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.Join(strings.Fields(s), "_")
	return s
}

func normalizeRDIStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "active"
	}
	switch s {
	case "active", "actief", "in_gebruik", "in gebruik", "valid":
		return "active"
	case "expired", "verlopen", "inactief", "inactive":
		return "expired"
	case "cancelled", "ingetrokken", "revoked":
		return "cancelled"
	default:
		return strings.ReplaceAll(s, " ", "_")
	}
}

func toJSONString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(strings.ToValidUTF8(t, ""))
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}
