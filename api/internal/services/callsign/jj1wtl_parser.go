package callsign

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	// JJ1WTLLicenseSearchURL is the JJ1WTL landing page that lists annual CSV exports
	// of the Japanese MIC amateur callsign database.
	// Community source maintained by JJ1WTL: http://motobayashi.net/callsign/licensesearch.html
	JJ1WTLLicenseSearchURL = "http://motobayashi.net/callsign/licensesearch.html"

	jj1wtlSource = "jj1wtl"
)

var jj1wtlCSVURLRe = regexp.MustCompile(`https?://[^\s"'<>]+/offline-callbook-ja-(\d{8})-en\.csv`)

// ParseJJ1WTL discovers the latest JJ1WTL CSV URL from the landing page,
// downloads it, and parses normalized callsign records.
func ParseJJ1WTL(ctx context.Context, indexURL string) (*ParseResult, error) {
	slog.InfoContext(ctx, "jj1wtl_parser: discovering latest CSV URL", slog.String("index_url", indexURL))

	csvURL, err := discoverJJ1WTLLatestCSVURL(ctx, indexURL)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "jj1wtl_parser: downloading CSV", slog.String("url", csvURL))
	data, err := downloadZip(ctx, csvURL)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", csvURL, err)
	}

	slog.InfoContext(ctx, "jj1wtl_parser: download complete",
		slog.String("url", csvURL),
		slog.Int("bytes", len(data)),
	)

	return ParseJJ1WTLCSVData(ctx, data)
}

func discoverJJ1WTLLatestCSVURL(ctx context.Context, indexURL string) (string, error) {
	body, err := downloadZip(ctx, indexURL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", indexURL, err)
	}

	url, err := discoverJJ1WTLLatestCSVURLFromHTML(string(body))
	if err != nil {
		return "", fmt.Errorf("discover latest JJ1WTL CSV URL: %w", err)
	}
	return url, nil
}

func discoverJJ1WTLLatestCSVURLFromHTML(html string) (string, error) {
	matches := jj1wtlCSVURLRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("no JJ1WTL CSV links found in landing page")
	}

	type candidate struct {
		url  string
		date string
	}
	candidates := make([]candidate, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		u := m[0]
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		candidates = append(candidates, candidate{url: u, date: m[1]})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no valid JJ1WTL CSV link candidates")
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].date == candidates[j].date {
			return candidates[i].url > candidates[j].url
		}
		return candidates[i].date > candidates[j].date
	})

	return candidates[0].url, nil
}

// ParseJJ1WTLCSVData parses JJ1WTL CSV bytes already loaded in memory.
// It tries UTF-8 first and falls back to Shift-JIS decoding.
func ParseJJ1WTLCSVData(ctx context.Context, data []byte) (*ParseResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty CSV payload")
	}

	utf8Result, utf8Err := parseJJ1WTLCSV(bytes.NewReader(data))
	if utf8Err == nil {
		slog.InfoContext(ctx, "jj1wtl_parser: parsed rows",
			slog.String("encoding", "utf-8"),
			slog.Int("processed", utf8Result.Processed),
			slog.Int("skipped", utf8Result.Skipped),
			slog.Int("records", len(utf8Result.Records)),
		)
		return utf8Result, nil
	}

	sjisReader := transform.NewReader(bytes.NewReader(data), japanese.ShiftJIS.NewDecoder())
	sjisResult, sjisErr := parseJJ1WTLCSV(sjisReader)
	if sjisErr == nil {
		slog.InfoContext(ctx, "jj1wtl_parser: parsed rows",
			slog.String("encoding", "shift-jis"),
			slog.Int("processed", sjisResult.Processed),
			slog.Int("skipped", sjisResult.Skipped),
			slog.Int("records", len(sjisResult.Records)),
		)
		return sjisResult, nil
	}

	return nil, fmt.Errorf("parse JJ1WTL CSV (UTF-8 failed: %v; Shift-JIS failed: %w)", utf8Err, sjisErr)
}

func parseJJ1WTLCSV(r io.Reader) (*ParseResult, error) {
	csvr := csv.NewReader(r)
	csvr.FieldsPerRecord = -1
	csvr.LazyQuotes = true
	csvr.TrimLeadingSpace = true

	header, err := csvr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	idx := buildJJ1WTLHeaderIndex(header)
	if _, ok := idx["callsign"]; !ok {
		return nil, fmt.Errorf("missing required callsign column")
	}

	result := &ParseResult{}
	for {
		row, err := csvr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}

		result.Processed++
		norm := normalizeJJ1WTLRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	return result, nil
}

func buildJJ1WTLHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int)

	set := func(key string, i int) {
		if _, exists := idx[key]; !exists {
			idx[key] = i
		}
	}

	for i, raw := range header {
		norm := normalizeJJ1WTLHeader(raw)
		switch norm {
		case "call", "callsign", "コールサイン", "呼出符号":
			set("callsign", i)
		case "prefecture", "都道府県", "都道府県名":
			set("prefecture", i)
		case "city/gun", "city", "市区町村", "市郡", "city or gun":
			set("city", i)
		case "ward/town/village", "ward", "town/village", "区町村", "町村", "ward or town or village":
			set("ward", i)
		case "license class and fixed/mobile", "license class", "license class and fixed or mobile", "免許種別":
			set("license_class", i)
		case "licensed/renewed date (5-year valid)", "licensed/renewed date", "licensed date", "renewed date", "免許年月日":
			set("grant_date", i)
		case "licensee (disclosed only in case of a club station)", "licensee", "name", "名前", "氏名":
			set("full_name", i)
		}
	}

	return idx
}

func normalizeJJ1WTLRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(jj1wtlField(row, idx, "callsign"))
	if callsign == "" {
		return nil
	}

	prefecture := jj1wtlField(row, idx, "prefecture")
	city := jj1wtlField(row, idx, "city")
	ward := jj1wtlField(row, idx, "ward")
	fullName := jj1wtlField(row, idx, "full_name")
	licenseClass := jj1wtlField(row, idx, "license_class")
	grantDate := parseJJ1WTLDate(jj1wtlField(row, idx, "grant_date"))

	locality := city
	if ward != "" {
		if locality == "" {
			locality = ward
		} else {
			locality = strings.TrimSpace(locality + " " + ward)
		}
	}

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        jj1wtlSource,
		FullName:      fullName,
		City:          locality,
		StateProvince: prefecture,
		Country:       "Japan",
		LicenseClass:  licenseClass,
		GrantDate:     grantDate,
		Status:        "active",
	}
}

func jj1wtlField(row []string, idx map[string]int, col string) string {
	i, ok := idx[col]
	if !ok || i >= len(row) {
		return ""
	}
	v := strings.TrimSpace(row[i])
	v = strings.ToValidUTF8(v, "")
	return v
}

func normalizeJJ1WTLHeader(h string) string {
	h = strings.TrimSpace(strings.ToValidUTF8(h, ""))
	h = strings.ReplaceAll(h, "\u3000", " ")
	h = strings.ToLower(h)
	h = strings.Join(strings.Fields(h), " ")
	return h
}

func parseJJ1WTLDate(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	layouts := []string{"2006-01-02", "2006/01/02", "2006.01.02"}
	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return &t
		}
	}
	return nil
}
