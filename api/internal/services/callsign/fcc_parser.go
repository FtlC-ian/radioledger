// Package callsign provides FCC ULS amateur radio license database parsing
// and River workers for keeping callsign_records up to date.
//
// FCC data format:
//   - Full dump: https://data.fcc.gov/download/pub/uls/complete/l_amat.zip
//   - Daily diff: https://data.fcc.gov/download/pub/uls/daily/l_am_*.zip
//   - Files are pipe-delimited (|), joined on unique_system_identifier (col 1)
//
// Key files inside the zip:
//   - EN.dat  — Entity: name, address, FRN
//   - HD.dat  — License header: callsign, status, grant date, expiry date
//   - AM.dat  — Amateur specific: operator class
package callsign

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	// FCCFullDumpURL is the URL for the FCC ULS complete amateur radio dump.
	FCCFullDumpURL = "https://data.fcc.gov/download/pub/uls/complete/l_amat.zip"
	// FCCDailyDumpURLFmt is the URL pattern for FCC daily diff files.
	// The day suffix is one of: mon, tue, wed, thu, fri, sat.
	FCCDailyDumpURLFmt = "https://data.fcc.gov/download/pub/uls/daily/l_am_%s.zip"

	fccSource = "fcc"
)

// FCCRecord is a parsed amateur radio license record from the FCC ULS database.
type FCCRecord struct {
	// From HD.dat
	UniqueSystemID   string
	Callsign         string
	LicenseStatus    string // 'A'=active, 'E'=expired, 'C'=cancelled
	GrantDate        string // MMDDYYYY or empty
	ExpiredDate      string // MMDDYYYY or empty
	CancellationDate string

	// From EN.dat
	FirstName    string
	LastName     string
	FullName     string
	AddressLine1 string
	AddressLine2 string
	City         string
	State        string
	ZipCode      string
	FRN          string // FCC Registration Number

	// From AM.dat
	OperatorClass string // 'A'=Advanced, 'E'=Extra, 'G'=General, 'N'=Novice, 'T'=Technician
	GroupCode     string
	RegionCode    string
}

// NormalizedRecord converts an FCCRecord into a CallsignRecord ready for database upsert.
type NormalizedRecord struct {
	Callsign      string
	Source        string
	SourceID      string // FRN
	FirstName     string
	LastName      string
	FullName      string
	AddressLine1  string
	AddressLine2  string
	City          string
	StateProvince string
	PostalCode    string
	Country       string
	LicenseClass  string
	GrantDate     *time.Time
	ExpiryDate    *time.Time
	Status        string
	Latitude      *float64
	Longitude     *float64
}

// ParseResult holds parsing statistics along with the parsed records.
type ParseResult struct {
	Records   []NormalizedRecord
	Processed int
	Skipped   int
}

// ParseFCCZip downloads and parses an FCC ULS zip file (full or daily).
// ctx controls the HTTP request lifetime; progress is logged via slog.
func ParseFCCZip(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "fcc_parser: downloading zip", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "fcc_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseFCCZipData(ctx, data)
}

// ParseFCCZipData parses a zip file already in memory (useful for testing).
func ParseFCCZipData(ctx context.Context, data []byte) (*ParseResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	// Collect pipe-delimited rows from each file.
	hdRows := make(map[string][]string) // unique_system_id → fields
	enRows := make(map[string][]string)
	amRows := make(map[string][]string)

	for _, f := range zr.File {
		name := strings.ToUpper(f.Name)
		switch {
		case strings.HasSuffix(name, "HD.DAT"):
			// keyCol=1 → unique_system_identifier (col 0 is record type "HD")
			if err := readPipeFile(f, hdRows, 1); err != nil {
				return nil, fmt.Errorf("read HD.dat: %w", err)
			}
		case strings.HasSuffix(name, "EN.DAT"):
			if err := readPipeFile(f, enRows, 1); err != nil {
				return nil, fmt.Errorf("read EN.dat: %w", err)
			}
		case strings.HasSuffix(name, "AM.DAT"):
			if err := readPipeFile(f, amRows, 1); err != nil {
				return nil, fmt.Errorf("read AM.dat: %w", err)
			}
		}
	}

	slog.InfoContext(ctx, "fcc_parser: parsed raw rows",
		slog.Int("hd_rows", len(hdRows)),
		slog.Int("en_rows", len(enRows)),
		slog.Int("am_rows", len(amRows)),
	)

	result := &ParseResult{}

	for sysID, hd := range hdRows {
		result.Processed++

		rec := joinRows(sysID, hd, enRows[sysID], amRows[sysID])
		if rec == nil {
			result.Skipped++
			continue
		}

		norm := normalize(rec)
		if norm == nil {
			result.Skipped++
			continue
		}

		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "fcc_parser: join complete",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Internals
// ─────────────────────────────────────────────────────────────────────────────

// readPipeFile reads a pipe-delimited file from the zip into the rows map,
// keyed by the field at keyCol (usually 0 = unique_system_identifier).
func readPipeFile(f *zip.File, rows map[string][]string, keyCol int) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	scanner := bufio.NewScanner(rc)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) <= keyCol {
			continue
		}
		key := strings.TrimSpace(fields[keyCol])
		if key == "" {
			continue
		}
		rows[key] = fields
	}
	return scanner.Err()
}

// HD.dat column indices (0-based, pipe-delimited).
// Record Type|Unique System Identifier|ULS File Number|EBF Number|Call Sign|
// License Status|Radio Service Code|Grant Date|Expired Date|Cancellation Date|
// Eligibility Rule Num|...
const (
	hdCallSign         = 4
	hdLicenseStatus    = 5
	hdGrantDate        = 7
	hdExpiredDate      = 8
	hdCancellationDate = 9
)

// EN.dat column indices.
// Record Type|Unique System Identifier|ULS File Number|EBF Number|Call Sign|
// Entity Type|Licensee ID|Entity Name|First Name|MI|Last Name|Suffix|
// Phone|Fax|Email|Street Address|City|State|Zip Code|PO Box|
// Attention Line|FRN|...
const (
	enEntityName = 7
	enFirstName  = 8
	enLastName   = 10
	enAddr       = 15
	enCity       = 16
	enState      = 17
	enZip        = 18
	enFRN        = 21
)

// AM.dat column indices.
// Record Type|Unique System Identifier|ULS File Number|EBF Number|Call Sign|
// Operator Class|Group Code|Region Code|...
const (
	amOperatorClass = 5
	amGroupCode     = 6
	amRegionCode    = 7
)

func safeField(fields []string, idx int) string {
	if fields == nil || idx >= len(fields) {
		return ""
	}
	return strings.TrimSpace(fields[idx])
}

// joinRows joins HD, EN, and AM rows by unique_system_identifier into an FCCRecord.
// Returns nil if the record should be skipped (no HD entry).
func joinRows(sysID string, hd, en, am []string) *FCCRecord {
	if hd == nil {
		return nil
	}

	rec := &FCCRecord{
		UniqueSystemID:   sysID,
		Callsign:         strings.ToUpper(safeField(hd, hdCallSign)),
		LicenseStatus:    safeField(hd, hdLicenseStatus),
		GrantDate:        safeField(hd, hdGrantDate),
		ExpiredDate:      safeField(hd, hdExpiredDate),
		CancellationDate: safeField(hd, hdCancellationDate),
	}

	if en != nil {
		entityName := safeField(en, enEntityName)
		firstName := safeField(en, enFirstName)
		lastName := safeField(en, enLastName)
		rec.FirstName = firstName
		rec.LastName = lastName
		rec.FullName = buildFullName(firstName, lastName, entityName)
		rec.AddressLine1 = safeField(en, enAddr)
		rec.City = safeField(en, enCity)
		rec.State = safeField(en, enState)
		rec.ZipCode = safeField(en, enZip)
		rec.FRN = safeField(en, enFRN)
	}

	if am != nil {
		rec.OperatorClass = safeField(am, amOperatorClass)
		rec.GroupCode = safeField(am, amGroupCode)
		rec.RegionCode = safeField(am, amRegionCode)
	}

	return rec
}

func buildFullName(first, last, entity string) string {
	if first != "" && last != "" {
		return strings.TrimSpace(first + " " + last)
	}
	if last != "" {
		return last
	}
	return entity
}

// normalize converts an FCCRecord to a NormalizedRecord ready for DB upsert.
// Returns nil for records with no callsign (invalid).
func normalize(rec *FCCRecord) *NormalizedRecord {
	if rec.Callsign == "" {
		return nil
	}

	status := fccStatusToStatus(rec.LicenseStatus, rec.CancellationDate)
	licenseClass := fccClassToClass(rec.OperatorClass)

	norm := &NormalizedRecord{
		Callsign:      rec.Callsign,
		Source:        fccSource,
		SourceID:      rec.FRN,
		FirstName:     rec.FirstName,
		LastName:      rec.LastName,
		FullName:      rec.FullName,
		AddressLine1:  rec.AddressLine1,
		City:          rec.City,
		StateProvince: rec.State,
		PostalCode:    rec.ZipCode,
		Country:       "US",
		LicenseClass:  licenseClass,
		Status:        status,
		GrantDate:     parseFCCDate(rec.GrantDate),
		ExpiryDate:    parseFCCDate(rec.ExpiredDate),
	}

	return norm
}

// fccStatusToStatus maps FCC license status codes to our internal status values.
func fccStatusToStatus(status, cancellationDate string) string {
	if cancellationDate != "" {
		return "cancelled"
	}
	switch strings.ToUpper(status) {
	case "A":
		return "active"
	case "E":
		return "expired"
	case "C":
		return "cancelled"
	default:
		return "active"
	}
}

// fccClassToClass maps FCC operator class codes to human-readable names.
func fccClassToClass(code string) string {
	switch strings.ToUpper(code) {
	case "A":
		return "advanced"
	case "E":
		return "extra"
	case "G":
		return "general"
	case "N":
		return "novice"
	case "T":
		return "technician"
	case "P":
		return "technician_plus"
	default:
		return ""
	}
}

// parseFCCDate parses FCC date strings in MMDDYYYY format.
func parseFCCDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse("01/02/2006", s)
	if err != nil {
		// Try MMDDYYYY without slashes
		t, err = time.Parse("01022006", s)
		if err != nil {
			return nil
		}
	}
	t = t.UTC()
	return &t
}

// downloadZip performs an HTTP GET and returns the full body as bytes.
func downloadZip(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "RadioLedger/1.0 FCC-ULS-Sync (+https://radioledger.com)")

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
