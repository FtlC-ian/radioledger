package callsign_test

import (
	"context"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
	"golang.org/x/text/encoding/charmap"
)

func TestParseIFTCSVData_BasicRecord(t *testing.T) {
	csvData := []byte("indicativo,nombre,estado,municipio,fecha_otorgamiento,fecha_vencimiento\nXE1ABC,José Pérez,Ciudad de México,Benito Juárez,2020-05-01,2030-05-01\n")

	result, err := callsign.ParseIFTCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseIFTCSVData: %v", err)
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
	if rec.Callsign != "XE1ABC" {
		t.Errorf("callsign: got %q, want XE1ABC", rec.Callsign)
	}
	if rec.Source != "ift" {
		t.Errorf("source: got %q, want ift", rec.Source)
	}
	if rec.FullName != "José Pérez" {
		t.Errorf("full_name: got %q, want José Pérez", rec.FullName)
	}
	if rec.StateProvince != "Ciudad de México" {
		t.Errorf("state_province: got %q, want Ciudad de México", rec.StateProvince)
	}
	if rec.City != "Benito Juárez" {
		t.Errorf("city: got %q, want Benito Juárez", rec.City)
	}
	if rec.Country != "Mexico" {
		t.Errorf("country: got %q, want Mexico", rec.Country)
	}
	if rec.GrantDate == nil || rec.GrantDate.Format("2006-01-02") != "2020-05-01" {
		t.Errorf("grant_date: got %v, want 2020-05-01", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || rec.ExpiryDate.Format("2006-01-02") != "2030-05-01" {
		t.Errorf("expiry_date: got %v, want 2030-05-01", rec.ExpiryDate)
	}
}

func TestParseIFTCSVData_Latin1SemicolonHeaders(t *testing.T) {
	input := "Indicativo;Nombre del Titular;Entidad Federativa;Municipio\nXE2ÑZ;Muñoz García;Nuevo León;Monterrey\n"
	latin1, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(input))
	if err != nil {
		t.Fatalf("latin1 encode: %v", err)
	}

	result, err := callsign.ParseIFTCSVData(context.Background(), latin1)
	if err != nil {
		t.Fatalf("ParseIFTCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "XE2ÑZ" {
		t.Errorf("callsign: got %q, want XE2ÑZ", rec.Callsign)
	}
	if rec.FullName != "Muñoz García" {
		t.Errorf("full_name: got %q, want Muñoz García", rec.FullName)
	}
	if rec.StateProvince != "Nuevo León" {
		t.Errorf("state_province: got %q, want Nuevo León", rec.StateProvince)
	}
	if rec.City != "Monterrey" {
		t.Errorf("city: got %q, want Monterrey", rec.City)
	}
}

func TestParseIFTCSVData_SkipsRowsWithoutValidCallsign(t *testing.T) {
	csvData := []byte("indicativo,nombre,estado,municipio\n,Sin Llamada,CDMX,Coyoacán\nXE3OK,Operador Válido,Jalisco,Guadalajara\n")

	result, err := callsign.ParseIFTCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseIFTCSVData: %v", err)
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
	if result.Records[0].Callsign != "XE3OK" {
		t.Errorf("callsign: got %q, want XE3OK", result.Records[0].Callsign)
	}
}

func TestParseIFTCSVData_RejectsHTMLPayload(t *testing.T) {
	_, err := callsign.ParseIFTCSVData(context.Background(), []byte("<!doctype html><html><body>blocked</body></html>"))
	if err == nil {
		t.Fatal("expected error for html payload")
	}
}
