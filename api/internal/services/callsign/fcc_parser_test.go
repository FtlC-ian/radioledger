package callsign_test

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
)

// buildTestZip creates an in-memory zip with EN.dat, HD.dat, AM.dat.
func buildTestZip(t *testing.T, hdLines, enLines, amLines []string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	writeFile := func(name string, lines []string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip: create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
			t.Fatalf("zip: write %s: %v", name, err)
		}
	}

	writeFile("HD.dat", hdLines)
	writeFile("EN.dat", enLines)
	writeFile("AM.dat", amLines)

	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// FCC pipe-delimited format:
// HD: RecordType|SysID|ULSFileNum|EBFNum|CallSign|Status|RadioSvc|GrantDate|ExpiredDate|CancelDate|...
// EN: RecordType|SysID|ULSFileNum|EBFNum|CallSign|EntityType|LicenseeID|EntityName|FirstName|MI|LastName|Suffix|Phone|Fax|Email|Addr|City|State|Zip|POBox|Attn|FRN|...
// AM: RecordType|SysID|ULSFileNum|EBFNum|CallSign|OperatorClass|GroupCode|RegionCode|...

func TestParseFCCZipData_BasicRecord(t *testing.T) {
	hdLines := []string{
		// SysID=1234567, Call=KI5BRG, Status=A, GrantDate=03/15/2020, ExpiredDate=03/15/2030
		"HD|1234567|||KI5BRG|A|HA|03/15/2020|03/15/2030||||",
	}
	enLines := []string{
		// SysID=1234567, EntityName=Ferrell Ian, FirstName=Ian, LastName=Ferrell, Addr=123 Main St, City=Fayetteville, State=AR, Zip=72701, FRN=0012345678
		"EN|1234567|||KI5BRG||LIC||Ian|R|Ferrell||555-1234||test@test.com|123 Main St|Fayetteville|AR|72701|||0012345678",
	}
	amLines := []string{
		// SysID=1234567, Class=E (Extra)
		"AM|1234567|||KI5BRG|E|A|5",
	}

	data := buildTestZip(t, hdLines, enLines, amLines)
	result, err := callsign.ParseFCCZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseFCCZipData: %v", err)
	}

	if result.Processed != 1 {
		t.Errorf("processed: got %d, want 1", result.Processed)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "KI5BRG" {
		t.Errorf("callsign: got %q, want %q", rec.Callsign, "KI5BRG")
	}
	if rec.Source != "fcc" {
		t.Errorf("source: got %q, want %q", rec.Source, "fcc")
	}
	if rec.FirstName != "Ian" {
		t.Errorf("first_name: got %q, want %q", rec.FirstName, "Ian")
	}
	if rec.LastName != "Ferrell" {
		t.Errorf("last_name: got %q, want %q", rec.LastName, "Ferrell")
	}
	if rec.City != "Fayetteville" {
		t.Errorf("city: got %q, want %q", rec.City, "Fayetteville")
	}
	if rec.StateProvince != "AR" {
		t.Errorf("state: got %q, want %q", rec.StateProvince, "AR")
	}
	if rec.Country != "US" {
		t.Errorf("country: got %q, want %q", rec.Country, "US")
	}
	if rec.LicenseClass != "extra" {
		t.Errorf("license_class: got %q, want %q", rec.LicenseClass, "extra")
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want %q", rec.Status, "active")
	}
	if rec.GrantDate == nil {
		t.Error("grant_date: expected non-nil")
	} else if rec.GrantDate.Year() != 2020 || rec.GrantDate.Month() != 3 || rec.GrantDate.Day() != 15 {
		t.Errorf("grant_date: got %v, want 2020-03-15", *rec.GrantDate)
	}
}

func TestParseFCCZipData_ExpiredRecord(t *testing.T) {
	hdLines := []string{
		"HD|9999999|||W5OLD|E|HA|01/01/2000|01/01/2010||||",
	}
	enLines := []string{
		"EN|9999999|||W5OLD||LIC||John||Doe||||||Anywhere||TX|75001|||",
	}
	amLines := []string{
		"AM|9999999|||W5OLD|G|A|5",
	}

	data := buildTestZip(t, hdLines, enLines, amLines)
	result, err := callsign.ParseFCCZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseFCCZipData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Status != "expired" {
		t.Errorf("status: got %q, want %q", rec.Status, "expired")
	}
	if rec.LicenseClass != "general" {
		t.Errorf("license_class: got %q, want %q", rec.LicenseClass, "general")
	}
}

func TestParseFCCZipData_CancelledRecord(t *testing.T) {
	hdLines := []string{
		// Cancelled: status=C and cancellation date set
		"HD|1111111|||KA5OLD|C|HA|01/01/1990|01/01/2000|06/01/1999|||",
	}
	enLines := []string{
		"EN|1111111|||KA5OLD||LIC||Jane||Smith||||||Smalltown||OK|74001|||",
	}
	amLines := []string{
		"AM|1111111|||KA5OLD|T|A|5",
	}

	data := buildTestZip(t, hdLines, enLines, amLines)
	result, err := callsign.ParseFCCZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseFCCZipData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Status != "cancelled" {
		t.Errorf("status: got %q, want %q", rec.Status, "cancelled")
	}
	if rec.LicenseClass != "technician" {
		t.Errorf("license_class: got %q, want %q", rec.LicenseClass, "technician")
	}
}

func TestParseFCCZipData_MissingENRow(t *testing.T) {
	// HD exists, EN missing — should still produce a record (with empty name fields).
	hdLines := []string{
		"HD|2222222|||W1TEST|A|HA|06/01/2015|06/01/2025||||",
	}
	enLines := []string{} // no EN entry
	amLines := []string{
		"AM|2222222|||W1TEST|E|A|1",
	}

	data := buildTestZip(t, hdLines, enLines, amLines)
	result, err := callsign.ParseFCCZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseFCCZipData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "W1TEST" {
		t.Errorf("callsign: got %q, want %q", rec.Callsign, "W1TEST")
	}
	if rec.Country != "US" {
		t.Errorf("country: got %q, want %q", rec.Country, "US")
	}
}

func TestParseFCCZipData_MultipleRecords(t *testing.T) {
	hdLines := []string{
		"HD|3000001|||KI5AAA|A|HA|01/01/2021|01/01/2031||||",
		"HD|3000002|||KI5BBB|A|HA|02/01/2021|02/01/2031||||",
		"HD|3000003|||KI5CCC|E|HA|03/01/2015|03/01/2025||||",
	}
	enLines := []string{
		"EN|3000001|||KI5AAA||LIC||Alice||Adams||||||City1||TX|75001|||",
		"EN|3000002|||KI5BBB||LIC||Bob||Baker||||||City2||OK|74001|||",
		"EN|3000003|||KI5CCC||LIC||Carol||Clark||||||City3||NM|87101|||",
	}
	amLines := []string{
		"AM|3000001|||KI5AAA|T|A|5",
		"AM|3000002|||KI5BBB|G|A|5",
		"AM|3000003|||KI5CCC|E|A|5",
	}

	data := buildTestZip(t, hdLines, enLines, amLines)
	result, err := callsign.ParseFCCZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseFCCZipData: %v", err)
	}
	if len(result.Records) != 3 {
		t.Fatalf("records: got %d, want 3", len(result.Records))
	}
	if result.Processed != 3 {
		t.Errorf("processed: got %d, want 3", result.Processed)
	}
}

func TestParseFCCZipData_EmptyCallsignSkipped(t *testing.T) {
	// HD row with no callsign should be skipped.
	hdLines := []string{
		"HD|9888888||||A|HA|01/01/2020|01/01/2030||||", // empty callsign field
	}
	enLines := []string{}
	amLines := []string{}

	data := buildTestZip(t, hdLines, enLines, amLines)
	result, err := callsign.ParseFCCZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseFCCZipData: %v", err)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected 0 records for empty callsign, got %d", len(result.Records))
	}
}
