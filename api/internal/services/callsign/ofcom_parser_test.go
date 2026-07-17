package callsign_test

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
)

func TestParseOfcomData_CSVBasicRecord(t *testing.T) {
	csvData := []byte("callsign,status\nM7ABC,assigned\n")

	result, err := callsign.ParseOfcomData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseOfcomData: %v", err)
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
	if rec.Callsign != "M7ABC" {
		t.Errorf("callsign: got %q, want M7ABC", rec.Callsign)
	}
	if rec.Source != "ofcom" {
		t.Errorf("source: got %q, want ofcom", rec.Source)
	}
	if rec.Country != "United Kingdom" {
		t.Errorf("country: got %q, want United Kingdom", rec.Country)
	}
	if rec.Status != "assigned" {
		t.Errorf("status: got %q, want assigned", rec.Status)
	}
}

func TestParseOfcomData_ZipWithCSV(t *testing.T) {
	csvData := "Call Sign,Status\n2E0XYZ,available\n"

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("ofcom_callsigns.csv")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := f.Write([]byte(csvData)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	result, err := callsign.ParseOfcomData(context.Background(), buf.Bytes())
	if err != nil {
		t.Fatalf("ParseOfcomData: %v", err)
	}
	if result.Processed != 1 {
		t.Fatalf("processed: got %d, want 1", result.Processed)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}
	if result.Records[0].Callsign != "2E0XYZ" {
		t.Errorf("callsign: got %q, want 2E0XYZ", result.Records[0].Callsign)
	}
	if result.Records[0].Status != "available" {
		t.Errorf("status: got %q, want available", result.Records[0].Status)
	}
}

func TestParseOfcomData_SkipsInvalidCallsignRows(t *testing.T) {
	csvData := []byte("callsign,status\ninvalid,assigned\nGM1AAA,Assigned\n")

	result, err := callsign.ParseOfcomData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseOfcomData: %v", err)
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
	if result.Records[0].Callsign != "GM1AAA" {
		t.Errorf("callsign: got %q, want GM1AAA", result.Records[0].Callsign)
	}
	if result.Records[0].Status != "assigned" {
		t.Errorf("status: got %q, want assigned", result.Records[0].Status)
	}
}
