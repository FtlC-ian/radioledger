package callsign_test

import (
	"context"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
	"golang.org/x/text/encoding/charmap"
)

func TestParseANFRCSVData_BasicRecord(t *testing.T) {
	csvData := []byte("indicatif,nom,prenom,adresse,code_postal,commune,departement,latitude,longitude\nF4ABC,Dupont,Jean,10 Rue de Paris,75001,Paris,75,48.8566,2.3522\n")

	result, err := callsign.ParseANFRCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANFRCSVData: %v", err)
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
	if rec.Callsign != "F4ABC" {
		t.Errorf("callsign: got %q, want F4ABC", rec.Callsign)
	}
	if rec.Source != "anfr" {
		t.Errorf("source: got %q, want anfr", rec.Source)
	}
	if rec.FirstName != "Jean" {
		t.Errorf("first_name: got %q, want Jean", rec.FirstName)
	}
	if rec.LastName != "Dupont" {
		t.Errorf("last_name: got %q, want Dupont", rec.LastName)
	}
	if rec.FullName != "Jean Dupont" {
		t.Errorf("full_name: got %q, want Jean Dupont", rec.FullName)
	}
	if rec.City != "Paris" {
		t.Errorf("city: got %q, want Paris", rec.City)
	}
	if rec.StateProvince != "75" {
		t.Errorf("state_province: got %q, want 75", rec.StateProvince)
	}
	if rec.PostalCode != "75001" {
		t.Errorf("postal_code: got %q, want 75001", rec.PostalCode)
	}
	if rec.Country != "France" {
		t.Errorf("country: got %q, want France", rec.Country)
	}
	if rec.Latitude == nil || *rec.Latitude != 48.8566 {
		t.Errorf("latitude: got %v, want 48.8566", rec.Latitude)
	}
	if rec.Longitude == nil || *rec.Longitude != 2.3522 {
		t.Errorf("longitude: got %v, want 2.3522", rec.Longitude)
	}
}

func TestParseANFRCSVData_Latin1SemicolonHeaders(t *testing.T) {
	input := "indicatif;nom;prénom;adresse;code postal;commune;département;latitude;longitude\nF1XYZ;Lefèvre;Émile;12 Rue de l'Église;33000;Bordeaux;Gironde;44,8378;-0,5792\n"
	latin1, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(input))
	if err != nil {
		t.Fatalf("latin1 encode: %v", err)
	}

	result, err := callsign.ParseANFRCSVData(context.Background(), latin1)
	if err != nil {
		t.Fatalf("ParseANFRCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.FirstName != "Émile" {
		t.Errorf("first_name: got %q, want Émile", rec.FirstName)
	}
	if rec.LastName != "Lefèvre" {
		t.Errorf("last_name: got %q, want Lefèvre", rec.LastName)
	}
	if rec.StateProvince != "Gironde" {
		t.Errorf("state_province: got %q, want Gironde", rec.StateProvince)
	}
	if rec.Latitude == nil || *rec.Latitude != 44.8378 {
		t.Errorf("latitude: got %v, want 44.8378", rec.Latitude)
	}
	if rec.Longitude == nil || *rec.Longitude != -0.5792 {
		t.Errorf("longitude: got %v, want -0.5792", rec.Longitude)
	}
}

func TestParseANFRCSVData_SkipsRowsWithoutValidCallsign(t *testing.T) {
	csvData := []byte("indicatif,nom,prenom\nF0,Test,Bad\n,No,Callsign\nF6OKA,Good,Row\n")

	result, err := callsign.ParseANFRCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANFRCSVData: %v", err)
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
	if result.Records[0].Callsign != "F6OKA" {
		t.Errorf("callsign: got %q, want F6OKA", result.Records[0].Callsign)
	}
}
