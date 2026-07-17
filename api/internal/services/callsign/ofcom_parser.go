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
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	// OfcomFullDumpURL is the UK Ofcom amateur callsign open-data source.
	OfcomFullDumpURL = "https://www.ofcom.org.uk/siteassets/resources/documents/about-ofcom/opendata/amateur-radio-call-signs.csv"

	ofcomSource = "ofcom"
)

// ParseOfcom downloads and parses an Ofcom amateur callsign dataset.
// The source is expected to be CSV, but zip-wrapped CSV is also supported.
func ParseOfcom(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "ofcom_parser: downloading dataset", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "ofcom_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseOfcomData(ctx, data)
}

// ParseOfcomData parses Ofcom dataset bytes already loaded in memory.
func ParseOfcomData(ctx context.Context, data []byte) (*ParseResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty dataset")
	}

	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return parseOfcomZipData(ctx, data)
	}

	decoded := decodeOfcomText(data)
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

	idx := ofcomHeaderIndex(header)
	if ofcomFindColumn(idx, "callsign", "call_sign", "call sign", "call") == -1 {
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
		norm := normalizeOfcomRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "ofcom_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func parseOfcomZipData(ctx context.Context, data []byte) (*ParseResult, error) {
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
		if strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".txt") {
			if fallback == nil || f.UncompressedSize64 > fallback.UncompressedSize64 {
				fallback = f
			}
		}
	}
	if fallback == nil {
		return nil, fmt.Errorf("no CSV file found in Ofcom zip")
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

	return ParseOfcomData(ctx, payload)
}

func normalizeOfcomRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(ofcomField(row, idx,
		"callsign", "call_sign", "call sign", "call")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	status := normalizeOfcomStatus(ofcomField(row, idx, "status", "licence_status", "state"))
	if status == "" {
		status = "active"
	}

	return &NormalizedRecord{
		Callsign: callsign,
		Source:   ofcomSource,
		Country:  "United Kingdom",
		Status:   status,
	}
}

func normalizeOfcomStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.Join(strings.Fields(s), "_")
	return s
}

func ofcomField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeOfcomHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func ofcomHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeOfcomHeader(h)] = i
	}
	return idx
}

func ofcomFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeOfcomHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

func canonicalizeOfcomHeader(s string) string {
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

func decodeOfcomText(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
}
