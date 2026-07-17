package callsign_test

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
)

func buildNBTCTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestParseNBTCData_BasicEnglishHeaders(t *testing.T) {
	csvData := strings.Join([]string{
		"callsign,full_name,province,district,license_class,status,issue_date,expiry_date,license_no",
		"hs1abc,Somchai Jaidee,Bangkok,Chatuchak,Advanced,Active,2024-01-15,2029-01-14,LIC-1001",
	}, "\n") + "\n"

	result, err := callsign.ParseNBTCData(context.Background(), []byte(csvData))
	if err != nil {
		t.Fatalf("ParseNBTCData: %v", err)
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
	if rec.Callsign != "HS1ABC" {
		t.Errorf("callsign: got %q, want HS1ABC", rec.Callsign)
	}
	if rec.Source != "nbtc" {
		t.Errorf("source: got %q, want nbtc", rec.Source)
	}
	if rec.SourceID != "LIC-1001" {
		t.Errorf("source_id: got %q, want LIC-1001", rec.SourceID)
	}
	if rec.FullName != "Somchai Jaidee" {
		t.Errorf("full_name: got %q, want Somchai Jaidee", rec.FullName)
	}
	if rec.City != "Chatuchak" {
		t.Errorf("city: got %q, want Chatuchak", rec.City)
	}
	if rec.StateProvince != "Bangkok" {
		t.Errorf("state_province: got %q, want Bangkok", rec.StateProvince)
	}
	if rec.Country != "Thailand" {
		t.Errorf("country: got %q, want Thailand", rec.Country)
	}
	if rec.LicenseClass != "advanced" {
		t.Errorf("license_class: got %q, want advanced", rec.LicenseClass)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
	if rec.GrantDate == nil || rec.GrantDate.Format("2006-01-02") != "2024-01-15" {
		t.Errorf("grant_date: got %v, want 2024-01-15", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || rec.ExpiryDate.Format("2006-01-02") != "2029-01-14" {
		t.Errorf("expiry_date: got %v, want 2029-01-14", rec.ExpiryDate)
	}
}

func TestParseNBTCData_ThaiHeadersZipAndFiltering(t *testing.T) {
	data := buildNBTCTestZip(t, map[string]string{
		"nbtc.csv": strings.Join([]string{
			"สัญญาณเรียกขาน,ชื่อผู้รับอนุญาต,จังหวัด,อำเภอ,ประเภทใบอนุญาต,สถานะ,วันที่ออกใบอนุญาต,วันหมดอายุ,เลขที่ใบอนุญาต",
			"E21XYZ,สมชาย ใจดี,เชียงใหม่,เมืองเชียงใหม่,ขั้นต้น,หมดอายุ,13/06/2567,13/06/2572,TH-42",
			"9M2ABC,Not Thailand,Penang,George Town,Advanced,Active,2024-01-01,2029-01-01,NOPE",
			",Missing Call,Bangkok,Din Daeng,Advanced,Active,2024-01-01,2029-01-01,NOPE2",
		}, "\n") + "\n",
	})

	result, err := callsign.ParseNBTCData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseNBTCData: %v", err)
	}
	if result.Processed != 3 {
		t.Fatalf("processed: got %d, want 3", result.Processed)
	}
	if result.Skipped != 2 {
		t.Fatalf("skipped: got %d, want 2", result.Skipped)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "E21XYZ" {
		t.Errorf("callsign: got %q, want E21XYZ", rec.Callsign)
	}
	if rec.FullName != "สมชาย ใจดี" {
		t.Errorf("full_name: got %q, want สมชาย ใจดี", rec.FullName)
	}
	if rec.StateProvince != "เชียงใหม่" {
		t.Errorf("state_province: got %q, want เชียงใหม่", rec.StateProvince)
	}
	if rec.City != "เมืองเชียงใหม่" {
		t.Errorf("city: got %q, want เมืองเชียงใหม่", rec.City)
	}
	if rec.SourceID != "TH-42" {
		t.Errorf("source_id: got %q, want TH-42", rec.SourceID)
	}
	if rec.LicenseClass != "foundation" {
		t.Errorf("license_class: got %q, want foundation", rec.LicenseClass)
	}
	if rec.Status != "expired" {
		t.Errorf("status: got %q, want expired", rec.Status)
	}
	if rec.GrantDate == nil || !rec.GrantDate.Equal(time.Date(2024, 6, 13, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("grant_date: got %v, want 2024-06-13 UTC", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || !rec.ExpiryDate.Equal(time.Date(2029, 6, 13, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expiry_date: got %v, want 2029-06-13 UTC", rec.ExpiryDate)
	}
}
