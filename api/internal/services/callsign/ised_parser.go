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
)

const (
	// ISEDFullDumpURL is the daily-updated Canadian amateur callsign dump.
	// Source: https://ised-isde.canada.ca/site/amateur-radio-operator-certificate-services/en/downloads
	ISEDFullDumpURL = "https://apc-cap.ic.gc.ca/datafiles/amateur_delim.zip"

	isedSource = "ised"
)

// ParseISEDZip downloads and parses the ISED amateur callsign zip file.
func ParseISEDZip(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "ised_parser: downloading zip", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "ised_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseISEDZipData(ctx, data)
}

// ParseISEDZipData parses an ISED callsign zip already loaded in memory.
func ParseISEDZipData(ctx context.Context, data []byte) (*ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	dataFile := findISEDDataFile(zr)
	if dataFile == nil {
		return nil, fmt.Errorf("ISED data file not found in zip")
	}

	rc, err := dataFile.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dataFile.Name, err)
	}
	defer func() { _ = rc.Close() }()

	r := csv.NewReader(rc)
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := idx["callsign"]; !ok {
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
		norm := normalizeISEDRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "ised_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)
	return result, nil
}

func findISEDDataFile(zr *zip.Reader) *zip.File {
	var fallback *zip.File
	for _, f := range zr.File {
		name := strings.ToLower(filepath.Base(f.Name))
		switch {
		case name == "amateur_delim.txt":
			return f
		case strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".txt"):
			if strings.Contains(name, "readme") || strings.Contains(name, "lisezmoi") {
				continue
			}
			if fallback == nil || f.UncompressedSize64 > fallback.UncompressedSize64 {
				fallback = f
			}
		}
	}
	return fallback
}

func normalizeISEDRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(iField(row, idx, "callsign"))
	if callsign == "" {
		return nil
	}

	first := iField(row, idx, "first_name")
	last := iField(row, idx, "surname")
	fullName := strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
	if fullName == "" {
		club1 := iField(row, idx, "club_name")
		club2 := iField(row, idx, "club_name_2")
		fullName = strings.TrimSpace(strings.TrimSpace(club1) + " " + strings.TrimSpace(club2))
	}

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        isedSource,
		FirstName:     first,
		LastName:      last,
		FullName:      fullName,
		AddressLine1:  iField(row, idx, "address_line"),
		City:          iField(row, idx, "city"),
		StateProvince: strings.ToUpper(iField(row, idx, "prov_cd")),
		PostalCode:    normalizePostalCode(iField(row, idx, "postal_code")),
		Country:       "Canada",
		LicenseClass:  isedLicenseClass(row, idx),
		Status:        "active",
	}
}

func iField(row []string, idx map[string]int, col string) string {
	i, ok := idx[col]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
}

func isedLicenseClass(row []string, idx map[string]int) string {
	qualD := iField(row, idx, "qual_d")
	qualE := iField(row, idx, "qual_e")
	qualC := iField(row, idx, "qual_c")
	qualB := iField(row, idx, "qual_b")
	qualA := iField(row, idx, "qual_a")

	switch {
	case qualD != "":
		return "advanced"
	case qualE != "":
		return "basic_honours"
	case qualC != "":
		return "basic_12wpm"
	case qualB != "":
		return "basic_5wpm"
	case qualA != "":
		return "basic"
	default:
		return ""
	}
}

func normalizePostalCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	return s
}
