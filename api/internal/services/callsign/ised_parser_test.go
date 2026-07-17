package callsign_test

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
)

func buildISEDTestZip(t *testing.T, dataFilename string, lines []string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	writeFile := func(name string, content string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip: create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip: write %s: %v", name, err)
		}
	}

	writeFile(dataFilename, strings.Join(lines, "\n")+"\n")
	writeFile("readme_amat_delim.txt", "readme")

	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestParseISEDZipData_BasicRecord(t *testing.T) {
	lines := []string{
		"callsign;first_name;surname;address_line;city;prov_cd;postal_code;qual_a;qual_b;qual_c;qual_d;qual_e;club_name;club_name_2;club_address;club_city;club_prov_cd;club_postal_code",
		"VA1ABC;Allan George;Browne;197 PELHAM ST, PO BOX 1832;LUNENBURG;NS;B0J2C0;;;;;E;;;;;;;",
	}

	data := buildISEDTestZip(t, "amateur_delim.txt", lines)
	result, err := callsign.ParseISEDZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseISEDZipData: %v", err)
	}

	if result.Processed != 1 {
		t.Fatalf("processed: got %d, want 1", result.Processed)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "VA1ABC" {
		t.Errorf("callsign: got %q, want VA1ABC", rec.Callsign)
	}
	if rec.Source != "ised" {
		t.Errorf("source: got %q, want ised", rec.Source)
	}
	if rec.FirstName != "Allan George" {
		t.Errorf("first_name: got %q", rec.FirstName)
	}
	if rec.LastName != "Browne" {
		t.Errorf("surname: got %q", rec.LastName)
	}
	if rec.StateProvince != "NS" {
		t.Errorf("state_province: got %q, want NS", rec.StateProvince)
	}
	if rec.PostalCode != "B0J2C0" {
		t.Errorf("postal_code: got %q, want B0J2C0", rec.PostalCode)
	}
	if rec.Country != "Canada" {
		t.Errorf("country: got %q, want Canada", rec.Country)
	}
	if rec.LicenseClass != "basic_honours" {
		t.Errorf("license_class: got %q, want basic_honours", rec.LicenseClass)
	}
}

func TestParseISEDZipData_ClubAndSkippedRecord(t *testing.T) {
	lines := []string{
		"callsign;first_name;surname;address_line;city;prov_cd;postal_code;qual_a;qual_b;qual_c;qual_d;qual_e;club_name;club_name_2;club_address;club_city;club_prov_cd;club_postal_code",
		";; ; ; ; ; ; ; ; ; ; ; ; ; ; ; ; ",
		"VE3RAC;;;;TORONTO;ON;M4B1B3;A;;;;;Radio Amateurs of Canada;Toronto Club;;;;",
	}

	data := buildISEDTestZip(t, "subdir/custom_name.txt", lines)
	result, err := callsign.ParseISEDZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseISEDZipData: %v", err)
	}
	if result.Processed != 2 {
		t.Fatalf("processed: got %d, want 2", result.Processed)
	}
	if result.Skipped != 1 {
		t.Fatalf("skipped: got %d, want 1", result.Skipped)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "VE3RAC" {
		t.Errorf("callsign: got %q, want VE3RAC", rec.Callsign)
	}
	if rec.FullName != "Radio Amateurs of Canada Toronto Club" {
		t.Errorf("full_name: got %q", rec.FullName)
	}
	if rec.LicenseClass != "basic" {
		t.Errorf("license_class: got %q, want basic", rec.LicenseClass)
	}
}
