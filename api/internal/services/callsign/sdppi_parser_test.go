package callsign_test

import (
	"context"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
	"golang.org/x/text/encoding/charmap"
)

// ── Happy-path basic record ───────────────────────────────────────────────────

func TestParseSdppiCSVData_BasicRecord(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik,provinsi,kota,tingkat,tanggal_terbit,masa_laku,status\n" +
			"YB2ERL,Bambang Suryo W,Jawa Tengah,Semarang,Penegak (Extra Class),2018-06-01,2028-11-01,Aktif\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
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
	if rec.Callsign != "YB2ERL" {
		t.Errorf("callsign: got %q, want YB2ERL", rec.Callsign)
	}
	if rec.Source != "sdppi" {
		t.Errorf("source: got %q, want sdppi", rec.Source)
	}
	if rec.FullName != "Bambang Suryo W" {
		t.Errorf("full_name: got %q, want Bambang Suryo W", rec.FullName)
	}
	if rec.StateProvince != "Jawa Tengah" {
		t.Errorf("state_province: got %q, want Jawa Tengah", rec.StateProvince)
	}
	if rec.City != "Semarang" {
		t.Errorf("city: got %q, want Semarang", rec.City)
	}
	if rec.Country != "Indonesia" {
		t.Errorf("country: got %q, want Indonesia", rec.Country)
	}
	if rec.LicenseClass != "extra" {
		t.Errorf("license_class: got %q, want extra", rec.LicenseClass)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
}

// ── License class normalisation ───────────────────────────────────────────────

func TestParseSdppiCSVData_LicenseClasses(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik,tingkat\n" +
			"YB1AAA,Alice,Penegak (Extra Class)\n" +
			"YC2BBB,Bob,Penggalang (General)\n" +
			"YD3CCC,Carol,Siaga (Novice)\n" +
			"YE4DDD,Dave,PENEGAK\n" +
			"YF5EEE,Eve,SIAGA\n" +
			"YG6FFF,Frank,penggalang\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 6 {
		t.Fatalf("records: got %d, want 6", len(result.Records))
	}

	want := map[string]string{
		"YB1AAA": "extra",
		"YC2BBB": "general",
		"YD3CCC": "novice",
		"YE4DDD": "extra",
		"YF5EEE": "novice",
		"YG6FFF": "general",
	}
	for _, rec := range result.Records {
		if w, ok := want[rec.Callsign]; ok {
			if rec.LicenseClass != w {
				t.Errorf("callsign %s: license_class got %q, want %q", rec.Callsign, rec.LicenseClass, w)
			}
		} else {
			t.Errorf("unexpected callsign %q", rec.Callsign)
		}
	}
}

// ── Status normalisation ──────────────────────────────────────────────────────

func TestParseSdppiCSVData_StatusNormalisation(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik,status\n" +
			"YB1ACT,Alice,Aktif\n" +
			"YC1EXP,Bob,Tidak Aktif\n" +
			"YD1REV,Carol,Dicabut\n" +
			"YE1CAN,Dave,Dibatalkan\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 4 {
		t.Fatalf("records: got %d, want 4", len(result.Records))
	}

	for _, rec := range result.Records {
		switch rec.Callsign {
		case "YB1ACT":
			if rec.Status != "active" {
				t.Errorf("YB1ACT status: got %q, want active", rec.Status)
			}
		case "YC1EXP", "YD1REV", "YE1CAN":
			if rec.Status != "expired" {
				t.Errorf("%s status: got %q, want expired", rec.Callsign, rec.Status)
			}
		}
	}
}

// ── Semicolon delimiter ───────────────────────────────────────────────────────

func TestParseSdppiCSVData_SemicolonDelimiter(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan;nama_pemilik;provinsi\n" +
			"YH9ZZZ;Siti Rahayu;Papua\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}
	if result.Records[0].Callsign != "YH9ZZZ" {
		t.Errorf("callsign: got %q, want YH9ZZZ", result.Records[0].Callsign)
	}
	if result.Records[0].StateProvince != "Papua" {
		t.Errorf("state_province: got %q, want Papua", result.Records[0].StateProvince)
	}
}

// ── Alternative column names (community export formats) ───────────────────────

func TestParseSdppiCSVData_AlternativeColumnNames(t *testing.T) {
	csvData := []byte(
		"callsign,full_name,province,city,license_class,grant_date,expiry_date,status\n" +
			"YC3ABC,Budi Santoso,Jawa Barat,Bandung,General,2020-01-15,2030-01-15,active\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "YC3ABC" {
		t.Errorf("callsign: got %q, want YC3ABC", rec.Callsign)
	}
	if rec.FullName != "Budi Santoso" {
		t.Errorf("full_name: got %q, want Budi Santoso", rec.FullName)
	}
	if rec.StateProvince != "Jawa Barat" {
		t.Errorf("state_province: got %q, want Jawa Barat", rec.StateProvince)
	}
	if rec.LicenseClass != "general" {
		t.Errorf("license_class: got %q, want general", rec.LicenseClass)
	}
	if rec.GrantDate == nil || rec.GrantDate.Format("2006-01-02") != "2020-01-15" {
		t.Errorf("grant_date: got %v, want 2020-01-15", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || rec.ExpiryDate.Format("2006-01-02") != "2030-01-15" {
		t.Errorf("expiry_date: got %v, want 2030-01-15", rec.ExpiryDate)
	}
}

// ── Indonesian written month names ───────────────────────────────────────────

func TestParseSdppiCSVData_IndonesianMonthDate(t *testing.T) {
	// "NOPEMBER 2028" is the format returned by the IAR-IKRAP lookup portal.
	csvData := []byte(
		"tanda_panggilan,nama_pemilik,masa_laku\n" +
			"YB2ERL,Bambang Suryo W,NOPEMBER 2028\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.ExpiryDate == nil {
		t.Fatal("expiry_date: got nil, want 2028-11-01")
	}
	if rec.ExpiryDate.Format("2006-01") != "2028-11" {
		t.Errorf("expiry_date: got %s, want 2028-11-xx", rec.ExpiryDate.Format("2006-01"))
	}
}

// ── DD/MM/YYYY date format ────────────────────────────────────────────────────

func TestParseSdppiCSVData_DateFormatDDMMYYYY(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik,tanggal_terbit,masa_laku\n" +
			"YD4TST,Agus Susanto,15/06/2017,15/06/2027\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.GrantDate == nil || rec.GrantDate.Format("2006-01-02") != "2017-06-15" {
		t.Errorf("grant_date: got %v, want 2017-06-15", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || rec.ExpiryDate.Format("2006-01-02") != "2027-06-15" {
		t.Errorf("expiry_date: got %v, want 2027-06-15", rec.ExpiryDate)
	}
}

// ── Latin-1 encoding ──────────────────────────────────────────────────────────

func TestParseSdppiCSVData_Latin1Encoding(t *testing.T) {
	input := "tanda_panggilan,nama_pemilik,provinsi\nYB5RTL,Águs Wibøwo,Sumatera Selatan\n"
	latin1, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(input))
	if err != nil {
		t.Fatalf("latin1 encode: %v", err)
	}

	result, err := callsign.ParseSdppiCSVData(context.Background(), latin1)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}
	if result.Records[0].Callsign != "YB5RTL" {
		t.Errorf("callsign: got %q, want YB5RTL", result.Records[0].Callsign)
	}
}

// ── Skip rows without valid Indonesian callsign ───────────────────────────────

func TestParseSdppiCSVData_SkipsInvalidCallsigns(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik\n" +
			",No Callsign\n" +              // empty → skip
			"INVALID,Not A Callsign\n" +    // not a callsign pattern → skip
			"YB3XYZ,Valid Callsign\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
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
	if result.Records[0].Callsign != "YB3XYZ" {
		t.Errorf("callsign: got %q, want YB3XYZ", result.Records[0].Callsign)
	}
}

// ── Reject HTML payloads ──────────────────────────────────────────────────────

func TestParseSdppiCSVData_RejectsHTMLPayload(t *testing.T) {
	_, err := callsign.ParseSdppiCSVData(context.Background(),
		[]byte("<!doctype html><html><body>Session Expired</body></html>"))
	if err == nil {
		t.Fatal("expected error for HTML payload, got nil")
	}
}

// ── Missing callsign column ───────────────────────────────────────────────────

func TestParseSdppiCSVData_MissingCallsignColumn(t *testing.T) {
	csvData := []byte("nama_pemilik,provinsi\nBudi Santoso,Jawa Barat\n")
	_, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err == nil {
		t.Fatal("expected error for missing callsign column, got nil")
	}
}

// ── All Indonesian prefixes are accepted ─────────────────────────────────────

func TestParseSdppiCSVData_AllPrefixes(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik\n" +
			"YB0AA,Alpha\n" +
			"YC1BB,Beta\n" +
			"YD2CC,Gamma\n" +
			"YE3DD,Delta\n" +
			"YF4EE,Epsilon\n" +
			"YG5FF,Zeta\n" +
			"YH6GG,Eta\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if len(result.Records) != 7 {
		t.Fatalf("records: got %d, want 7 (one per YB–YH prefix)", len(result.Records))
	}
	for _, rec := range result.Records {
		if rec.Country != "Indonesia" {
			t.Errorf("record %s: country got %q, want Indonesia", rec.Callsign, rec.Country)
		}
		if rec.Source != "sdppi" {
			t.Errorf("record %s: source got %q, want sdppi", rec.Callsign, rec.Source)
		}
	}
}

// ── Multiple records round-trip ───────────────────────────────────────────────

func TestParseSdppiCSVData_MultipleRecords(t *testing.T) {
	csvData := []byte(
		"tanda_panggilan,nama_pemilik,provinsi,tingkat,status\n" +
			"YB1AA,Alice,DKI Jakarta,Penegak,Aktif\n" +
			"YC2BB,Bob,Jawa Barat,Penggalang,Aktif\n" +
			"YD3CC,Carol,Jawa Tengah,Siaga,Tidak Aktif\n" +
			"YE4DD,Dave,Jawa Timur,Penegak,Aktif\n",
	)

	result, err := callsign.ParseSdppiCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseSdppiCSVData: %v", err)
	}
	if result.Processed != 4 {
		t.Fatalf("processed: got %d, want 4", result.Processed)
	}
	if len(result.Records) != 4 {
		t.Fatalf("records: got %d, want 4", len(result.Records))
	}
	for _, rec := range result.Records {
		if rec.Country != "Indonesia" {
			t.Errorf("record %s: country got %q, want Indonesia", rec.Callsign, rec.Country)
		}
	}
}
