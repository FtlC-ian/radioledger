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
)

const (
	// ACMAFullDumpURL is the full ACMA Register of Radiocommunications Licences dump.
	// Source: https://www.acma.gov.au/radiocomms-licence-data
	ACMAFullDumpURL = "https://web.acma.gov.au/rrl-updates/spectra_rrl.zip"

	acmaSource = "acma"
)

type acmaLicence struct {
	LicenceNo       string
	ClientNo        string
	LicenceTypeName string
	LicenceCategory string
	DateIssued      string
	DateOfEffect    string
	DateOfExpiry    string
	StatusText      string
}

type acmaClient struct {
	ClientNo       string
	Licensee       string
	TradingName    string
	PostalSuburb   string
	PostalState    string
	PostalPostcode string
}

type acmaSite struct {
	SiteID   string
	Name     string
	State    string
	Postcode string
}

// ParseACMAZip downloads and parses the ACMA register zip file.
func ParseACMAZip(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "acma_parser: downloading zip", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "acma_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseACMAZipData(ctx, data)
}

// ParseACMAZipData parses an ACMA zip already loaded in memory.
func ParseACMAZipData(ctx context.Context, data []byte) (*ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	licenceRows, err := readACMALicences(zr)
	if err != nil {
		return nil, err
	}

	clientRows, err := readACMAClients(zr, licenceRows)
	if err != nil {
		return nil, err
	}

	siteRows, err := readACMASites(zr)
	if err != nil {
		return nil, err
	}

	result, err := readACMADeviceDetails(zr, licenceRows, clientRows, siteRows)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "acma_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

func readACMALicences(zr *zip.Reader) (map[string]acmaLicence, error) {
	f := findCSVByBaseName(zr, "licence.csv")
	if f == nil {
		return nil, fmt.Errorf("missing licence.csv")
	}

	r, err := openCSVReader(f)
	if err != nil {
		return nil, fmt.Errorf("open licence.csv: %w", err)
	}
	defer func() { _ = r.Close() }()

	header, err := r.csv.Read()
	if err != nil {
		return nil, fmt.Errorf("read licence.csv header: %w", err)
	}
	idx := indexMap(header)

	rows := map[string]acmaLicence{}
	for {
		row, err := r.csv.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read licence.csv row: %w", err)
		}

		if !strings.EqualFold(aField(row, idx, "LICENCE_TYPE_NAME"), "Amateur") {
			continue
		}

		licNo := aField(row, idx, "LICENCE_NO")
		if licNo == "" {
			continue
		}

		rows[licNo] = acmaLicence{
			LicenceNo:       licNo,
			ClientNo:        aField(row, idx, "CLIENT_NO"),
			LicenceTypeName: aField(row, idx, "LICENCE_TYPE_NAME"),
			LicenceCategory: aField(row, idx, "LICENCE_CATEGORY_NAME"),
			DateIssued:      aField(row, idx, "DATE_ISSUED"),
			DateOfEffect:    aField(row, idx, "DATE_OF_EFFECT"),
			DateOfExpiry:    aField(row, idx, "DATE_OF_EXPIRY"),
			StatusText:      aField(row, idx, "STATUS_TEXT"),
		}
	}

	return rows, nil
}

func readACMAClients(zr *zip.Reader, licences map[string]acmaLicence) (map[string]acmaClient, error) {
	f := findCSVByBaseName(zr, "client.csv")
	if f == nil {
		return nil, fmt.Errorf("missing client.csv")
	}

	needed := make(map[string]struct{}, len(licences))
	for _, lic := range licences {
		if lic.ClientNo != "" {
			needed[lic.ClientNo] = struct{}{}
		}
	}

	r, err := openCSVReader(f)
	if err != nil {
		return nil, fmt.Errorf("open client.csv: %w", err)
	}
	defer func() { _ = r.Close() }()

	header, err := r.csv.Read()
	if err != nil {
		return nil, fmt.Errorf("read client.csv header: %w", err)
	}
	idx := indexMap(header)

	rows := make(map[string]acmaClient, len(needed))
	for {
		row, err := r.csv.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read client.csv row: %w", err)
		}

		clientNo := aField(row, idx, "CLIENT_NO")
		if clientNo == "" {
			continue
		}
		if _, ok := needed[clientNo]; !ok {
			continue
		}

		rows[clientNo] = acmaClient{
			ClientNo:       clientNo,
			Licensee:       aField(row, idx, "LICENCEE"),
			TradingName:    aField(row, idx, "TRADING_NAME"),
			PostalSuburb:   aField(row, idx, "POSTAL_SUBURB"),
			PostalState:    aField(row, idx, "POSTAL_STATE"),
			PostalPostcode: aField(row, idx, "POSTAL_POSTCODE"),
		}
	}

	return rows, nil
}

func readACMASites(zr *zip.Reader) (map[string]acmaSite, error) {
	f := findCSVByBaseName(zr, "site.csv")
	if f == nil {
		return nil, fmt.Errorf("missing site.csv")
	}

	r, err := openCSVReader(f)
	if err != nil {
		return nil, fmt.Errorf("open site.csv: %w", err)
	}
	defer func() { _ = r.Close() }()

	header, err := r.csv.Read()
	if err != nil {
		return nil, fmt.Errorf("read site.csv header: %w", err)
	}
	idx := indexMap(header)

	rows := map[string]acmaSite{}
	for {
		row, err := r.csv.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read site.csv row: %w", err)
		}

		siteID := aField(row, idx, "SITE_ID")
		if siteID == "" {
			continue
		}

		rows[siteID] = acmaSite{
			SiteID:   siteID,
			Name:     aField(row, idx, "NAME"),
			State:    aField(row, idx, "STATE"),
			Postcode: aField(row, idx, "POSTCODE"),
		}
	}

	return rows, nil
}

func readACMADeviceDetails(
	zr *zip.Reader,
	licences map[string]acmaLicence,
	clients map[string]acmaClient,
	sites map[string]acmaSite,
) (*ParseResult, error) {
	f := findCSVByBaseName(zr, "device_details.csv")
	if f == nil {
		return nil, fmt.Errorf("missing device_details.csv")
	}

	r, err := openCSVReader(f)
	if err != nil {
		return nil, fmt.Errorf("open device_details.csv: %w", err)
	}
	defer func() { _ = r.Close() }()

	header, err := r.csv.Read()
	if err != nil {
		return nil, fmt.Errorf("read device_details.csv header: %w", err)
	}
	idx := indexMap(header)

	result := &ParseResult{}
	for {
		row, err := r.csv.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read device_details.csv row: %w", err)
		}

		licNo := aField(row, idx, "LICENCE_NO")
		lic, ok := licences[licNo]
		if !ok {
			continue
		}
		result.Processed++

		norm := normalizeACMARow(
			aField(row, idx, "CALL_SIGN"),
			lic,
			clients[lic.ClientNo],
			sites[aField(row, idx, "SITE_ID")],
		)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	return result, nil
}

func normalizeACMARow(callsign string, lic acmaLicence, client acmaClient, site acmaSite) *NormalizedRecord {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" {
		return nil
	}

	fullName := strings.TrimSpace(client.Licensee)
	if fullName == "" {
		fullName = strings.TrimSpace(client.TradingName)
	}

	city := strings.TrimSpace(client.PostalSuburb)
	if city == "" {
		city = strings.TrimSpace(site.Name)
	}

	state := strings.ToUpper(strings.TrimSpace(client.PostalState))
	if state == "" {
		state = strings.ToUpper(strings.TrimSpace(site.State))
	}

	postalCode := strings.TrimSpace(client.PostalPostcode)
	if postalCode == "" {
		postalCode = strings.TrimSpace(site.Postcode)
	}

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        acmaSource,
		SourceID:      strings.TrimSpace(lic.LicenceNo),
		FullName:      fullName,
		City:          city,
		StateProvince: state,
		PostalCode:    postalCode,
		Country:       "Australia",
		LicenseClass:  normalizeACMALicenseClass(lic.LicenceCategory),
		GrantDate:     parseACMADate(firstNonEmpty(lic.DateOfEffect, lic.DateIssued)),
		ExpiryDate:    parseACMADate(lic.DateOfExpiry),
		Status:        normalizeACMAStatus(lic.StatusText),
	}
}

func normalizeACMALicenseClass(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.Join(strings.Fields(s), "_")
	return s
}

func normalizeACMAStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "expired", "cancelled", "surrendered", "suspended":
		return "expired"
	case "granted", "issued", "active":
		return "active"
	default:
		return "active"
	}
}

func parseACMADate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type csvReaderWithCloser struct {
	csv *csv.Reader
	rc  io.ReadCloser
}

func (r *csvReaderWithCloser) Close() error {
	if r == nil || r.rc == nil {
		return nil
	}
	return r.rc.Close()
}

func openCSVReader(f *zip.File) (*csvReaderWithCloser, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}

	r := csv.NewReader(rc)
	r.Comma = ','
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	return &csvReaderWithCloser{csv: r, rc: rc}, nil
}

func findCSVByBaseName(zr *zip.Reader, base string) *zip.File {
	base = strings.ToLower(strings.TrimSpace(base))
	for _, f := range zr.File {
		if strings.ToLower(filepath.Base(f.Name)) == base {
			return f
		}
	}
	return nil
}

func indexMap(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.ToUpper(strings.TrimSpace(h))] = i
	}
	return idx
}

func aField(row []string, idx map[string]int, col string) string {
	i, ok := idx[strings.ToUpper(col)]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
}
