package callsign

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	// ANFRFullDumpURL is the ANFR amateur dataset CSV.
	// Source landing page: https://www.data.gouv.fr/fr/datasets/annuaire-des-radioamateurs-et-des-stations-radioelectriques-du-service-damateur/
	ANFRFullDumpURL = "https://data.anfr.fr/sites/default/files/dataset/744/269eb-7088-4583-b509-c398bba58f77/copy_obs_amateur_statistiques_departements.csv"

	anfrSource = "anfr"
)

// ParseANFRCSV downloads and parses the ANFR amateur CSV.
func ParseANFRCSV(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "anfr_parser: downloading csv", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "anfr_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseANFRCSVData(ctx, data)
}

// ParseANFRCSVData parses ANFR CSV bytes already loaded in memory.
func ParseANFRCSVData(ctx context.Context, data []byte) (*ParseResult, error) {
	decoded := decodeANFRText(data)

	r := csv.NewReader(bytes.NewReader(decoded))
	r.Comma = detectCSVDelimiter(decoded)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	idx := anfrHeaderIndex(header)
	callsignCol := anfrFindColumn(idx, "indicatif", "callsign", "call_sign")
	if callsignCol == -1 {
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
		norm := normalizeANFRRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "anfr_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func normalizeANFRRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(anfrField(row, idx, "indicatif", "callsign", "call_sign", "detail")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	firstName := anfrField(row, idx, "prenom", "first_name")
	lastName := anfrField(row, idx, "nom", "last_name", "surname")
	fullName := strings.TrimSpace(anfrField(row, idx, "full_name", "nom_prenom", "titulaire"))
	if fullName == "" {
		fullName = strings.TrimSpace(strings.TrimSpace(firstName) + " " + strings.TrimSpace(lastName))
	}

	lat := anfrParseFloatPtr(anfrField(row, idx, "latitude", "lat"))
	lon := anfrParseFloatPtr(anfrField(row, idx, "longitude", "lon", "lng"))

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        anfrSource,
		SourceID:      anfrField(row, idx, "source_id", "id", "identifiant"),
		FirstName:     firstName,
		LastName:      lastName,
		FullName:      fullName,
		AddressLine1:  anfrField(row, idx, "adresse", "address", "address_line1"),
		City:          anfrField(row, idx, "commune", "ville", "city"),
		StateProvince: anfrField(row, idx, "departement", "department", "region", "state_province"),
		PostalCode:    anfrField(row, idx, "code_postal", "postal_code", "cp"),
		Country:       "France",
		Status:        "active",
		Latitude:      lat,
		Longitude:     lon,
	}
}

func anfrField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeANFRHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func anfrHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeANFRHeader(h)] = i
	}
	return idx
}

func anfrFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeANFRHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

func canonicalizeANFRHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	replacer := strings.NewReplacer(
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"à", "a", "â", "a", "ä", "a",
		"î", "i", "ï", "i",
		"ô", "o", "ö", "o",
		"ù", "u", "û", "u", "ü", "u",
		"ç", "c",
		"'", " ", "’", " ", "-", " ", ".", " ",
	)
	s = replacer.Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.Join(parts, "_")
}

func decodeANFRText(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return decoded
}

func detectCSVDelimiter(data []byte) rune {
	line := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line = data[:i]
	}
	if bytes.Count(line, []byte(";")) > bytes.Count(line, []byte(",")) {
		return ';'
	}
	return ','
}

func anfrParseFloatPtr(s string) *float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func isLikelyCallsign(c string) bool {
	c = strings.TrimSpace(c)
	if len(c) < 3 || len(c) > 16 {
		return false
	}
	hasLetter := false
	hasDigit := false
	for _, r := range c {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}
