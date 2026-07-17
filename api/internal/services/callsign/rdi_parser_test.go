package callsign_test

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
)

func TestParseRDIData_CSVBasicRecord(t *testing.T) {
	csvData := []byte("roepnaam;naam;plaatsnaam;vergunningtype;status\nPA3XYZ;Jansen, Piet;Utrecht;Novice;Actief\n")

	result, err := callsign.ParseRDIData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseRDIData: %v", err)
	}
	if result.Processed != 1 {
		t.Fatalf("processed: got %d, want 1", result.Processed)
	}
	if result.Skipped != 0 {
		t.Fatalf("skipped: got %d, want 0", result.Skipped)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "PA3XYZ" {
		t.Errorf("callsign: got %q, want PA3XYZ", rec.Callsign)
	}
	if rec.Source != "rdi" {
		t.Errorf("source: got %q, want rdi", rec.Source)
	}
	if rec.FullName != "Jansen, Piet" {
		t.Errorf("full_name: got %q, want Jansen, Piet", rec.FullName)
	}
	if rec.City != "Utrecht" {
		t.Errorf("city: got %q, want Utrecht", rec.City)
	}
	if rec.Country != "Netherlands" {
		t.Errorf("country: got %q, want Netherlands", rec.Country)
	}
	if rec.LicenseClass != "novice" {
		t.Errorf("license_class: got %q, want novice", rec.LicenseClass)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
}

func TestParseRDIData_JSONWrappedRecords(t *testing.T) {
	jsonData := []byte(`{"records":[{"callSign":"PD1ABC","name":"Zoë de Vries","city":"Leiden","licenseType":"Full","status":"expired"},{"callSign":"bad"}]}`)

	result, err := callsign.ParseRDIData(context.Background(), jsonData)
	if err != nil {
		t.Fatalf("ParseRDIData: %v", err)
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
	if rec.Callsign != "PD1ABC" {
		t.Errorf("callsign: got %q, want PD1ABC", rec.Callsign)
	}
	if rec.FullName != "Zoë de Vries" {
		t.Errorf("full_name: got %q, want Zoë de Vries", rec.FullName)
	}
	if rec.City != "Leiden" {
		t.Errorf("city: got %q, want Leiden", rec.City)
	}
	if rec.LicenseClass != "full" {
		t.Errorf("license_class: got %q, want full", rec.LicenseClass)
	}
	if rec.Status != "expired" {
		t.Errorf("status: got %q, want expired", rec.Status)
	}
}

func TestParseRDIData_ZipWithCSV(t *testing.T) {
	csvData := "callsign,name,city,license_type,status\nPH2A,Test User,Den Haag,Novice,active\n"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("rdi_callsigns.csv")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := f.Write([]byte(csvData)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	result, err := callsign.ParseRDIData(context.Background(), buf.Bytes())
	if err != nil {
		t.Fatalf("ParseRDIData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}
	if result.Records[0].Callsign != "PH2A" {
		t.Errorf("callsign: got %q, want PH2A", result.Records[0].Callsign)
	}
}
