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

func buildACMATestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip: create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip: write %s: %v", name, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestParseACMAZipData_BasicRecord(t *testing.T) {
	data := buildACMATestZip(t, map[string]string{
		"licence.csv": strings.Join([]string{
			"LICENCE_NO,CLIENT_NO,SV_ID,SS_ID,LICENCE_TYPE_NAME,LICENCE_CATEGORY_NAME,DATE_ISSUED,DATE_OF_EFFECT,DATE_OF_EXPIRY,STATUS,STATUS_TEXT,AP_ID,AP_PRJ_IDENT,SHIP_NAME,BSL_NO,AWL_TYPE",
			"10052066/1,20028443,6,600,Amateur,Advanced,2024-06-13,2024-06-13,2026-06-13,1,Granted,,,,,",
		}, "\n") + "\n",
		"client.csv": strings.Join([]string{
			"CLIENT_NO,LICENCEE,TRADING_NAME,ACN,ABN,POSTAL_STREET,POSTAL_SUBURB,POSTAL_STATE,POSTAL_POSTCODE,CAT_ID,CLIENT_TYPE_ID,FEE_STATUS_ID",
			"20028443,Jane Citizen,,,,,MELBOURNE,VIC,3000,,,",
		}, "\n") + "\n",
		"site.csv": strings.Join([]string{
			"SITE_ID,LATITUDE,LONGITUDE,NAME,STATE,LICENSING_AREA_ID,POSTCODE,SITE_PRECISION,ELEVATION,HCIS_L2",
			"123,-37.81,144.96,Melbourne,VIC,,3000,,,",
		}, "\n") + "\n",
		"device_details.csv": strings.Join([]string{
			"LICENCE_NO,SITE_ID,CALL_SIGN",
			"10052066/1,123,VK3ABC",
		}, "\n") + "\n",
	})

	result, err := callsign.ParseACMAZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseACMAZipData: %v", err)
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
	if rec.Callsign != "VK3ABC" {
		t.Errorf("callsign: got %q, want VK3ABC", rec.Callsign)
	}
	if rec.Source != "acma" {
		t.Errorf("source: got %q, want acma", rec.Source)
	}
	if rec.SourceID != "10052066/1" {
		t.Errorf("source_id: got %q, want 10052066/1", rec.SourceID)
	}
	if rec.FullName != "Jane Citizen" {
		t.Errorf("full_name: got %q, want Jane Citizen", rec.FullName)
	}
	if rec.City != "MELBOURNE" {
		t.Errorf("city: got %q, want MELBOURNE", rec.City)
	}
	if rec.StateProvince != "VIC" {
		t.Errorf("state_province: got %q, want VIC", rec.StateProvince)
	}
	if rec.PostalCode != "3000" {
		t.Errorf("postal_code: got %q, want 3000", rec.PostalCode)
	}
	if rec.Country != "Australia" {
		t.Errorf("country: got %q, want Australia", rec.Country)
	}
	if rec.LicenseClass != "advanced" {
		t.Errorf("license_class: got %q, want advanced", rec.LicenseClass)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
	if rec.GrantDate == nil || rec.GrantDate.Format("2006-01-02") != "2024-06-13" {
		t.Errorf("grant_date: got %v, want 2024-06-13", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || rec.ExpiryDate.Format("2006-01-02") != "2026-06-13" {
		t.Errorf("expiry_date: got %v, want 2026-06-13", rec.ExpiryDate)
	}
}

func TestParseACMAZipData_SkipsNonAmateurAndBlankCallsign(t *testing.T) {
	data := buildACMATestZip(t, map[string]string{
		"licence.csv": strings.Join([]string{
			"LICENCE_NO,CLIENT_NO,LICENCE_TYPE_NAME,LICENCE_CATEGORY_NAME,DATE_ISSUED,DATE_OF_EFFECT,DATE_OF_EXPIRY,STATUS_TEXT",
			"L1,C1,Amateur,Foundation,2023-01-01,2023-01-01,2025-01-01,Expired",
			"L2,C2,Land Mobile,Land Mobile System,2023-01-01,2023-01-01,2025-01-01,Granted",
		}, "\n") + "\n",
		"client.csv": strings.Join([]string{
			"CLIENT_NO,LICENCEE,TRADING_NAME,POSTAL_SUBURB,POSTAL_STATE,POSTAL_POSTCODE",
			"C1,,Club Name,SYDNEY,NSW,2000",
		}, "\n") + "\n",
		"site.csv": strings.Join([]string{
			"SITE_ID,NAME,STATE,POSTCODE",
			"S1,Sydney,NSW,2000",
		}, "\n") + "\n",
		"device_details.csv": strings.Join([]string{
			"LICENCE_NO,SITE_ID,CALL_SIGN",
			"L1,S1,",
			"L2,S1,VK2ZZZ",
			"L1,S1,VK2AAA",
		}, "\n") + "\n",
	})

	result, err := callsign.ParseACMAZipData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseACMAZipData: %v", err)
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
	if rec.Callsign != "VK2AAA" {
		t.Errorf("callsign: got %q, want VK2AAA", rec.Callsign)
	}
	if rec.FullName != "Club Name" {
		t.Errorf("full_name fallback: got %q, want Club Name", rec.FullName)
	}
	if rec.Status != "expired" {
		t.Errorf("status: got %q, want expired", rec.Status)
	}
	if rec.LicenseClass != "foundation" {
		t.Errorf("license_class: got %q, want foundation", rec.LicenseClass)
	}
	if rec.ExpiryDate == nil || !rec.ExpiryDate.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expiry_date: got %v, want 2025-01-01 UTC", rec.ExpiryDate)
	}
}
