package callsign_test

import (
	"context"
	"testing"

	"github.com/FtlC-ian/radioledger/api/internal/services/callsign"
	"golang.org/x/text/encoding/charmap"
)

// ── Basic happy-path ──────────────────────────────────────────────────────────

func TestParseANATELCSVData_BasicRecord(t *testing.T) {
	csvData := []byte(
		"indicativo;nome;uf;municipio;categoria;data_inicio_vigencia;data_fim_vigencia;situacao\n" +
			"PY2ABC;José da Silva;SP;São Paulo;Classe A;2015-03-10;2025-03-10;Ativo\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
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
	if rec.Callsign != "PY2ABC" {
		t.Errorf("callsign: got %q, want PY2ABC", rec.Callsign)
	}
	if rec.Source != "anatel" {
		t.Errorf("source: got %q, want anatel", rec.Source)
	}
	if rec.FullName != "José da Silva" {
		t.Errorf("full_name: got %q, want José da Silva", rec.FullName)
	}
	if rec.StateProvince != "SP" {
		t.Errorf("state_province: got %q, want SP", rec.StateProvince)
	}
	if rec.City != "São Paulo" {
		t.Errorf("city: got %q, want São Paulo", rec.City)
	}
	if rec.Country != "Brazil" {
		t.Errorf("country: got %q, want Brazil", rec.Country)
	}
	if rec.LicenseClass != "class_a" {
		t.Errorf("license_class: got %q, want class_a", rec.LicenseClass)
	}
	if rec.GrantDate == nil || rec.GrantDate.Format("2006-01-02") != "2015-03-10" {
		t.Errorf("grant_date: got %v, want 2015-03-10", rec.GrantDate)
	}
	if rec.ExpiryDate == nil || rec.ExpiryDate.Format("2006-01-02") != "2025-03-10" {
		t.Errorf("expiry_date: got %v, want 2025-03-10", rec.ExpiryDate)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
}

// ── License class normalisation ───────────────────────────────────────────────

func TestParseANATELCSVData_LicenseClasses(t *testing.T) {
	csvData := []byte(
		"indicativo;nome;uf;municipio;categoria;data_inicio_vigencia;data_fim_vigencia\n" +
			"PY1AAA;Alpha;RJ;Rio de Janeiro;Classe A;2018-01-01;2028-01-01\n" +
			"PY1BBB;Beta;RJ;Niteroi;Classe B;2018-01-01;2028-01-01\n" +
			"PY1CCC;Charlie;RJ;Petrópolis;Classe C;2018-01-01;2028-01-01\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
	}
	if len(result.Records) != 3 {
		t.Fatalf("records: got %d, want 3", len(result.Records))
	}

	classes := map[string]string{
		"PY1AAA": "class_a",
		"PY1BBB": "class_b",
		"PY1CCC": "class_c",
	}
	for _, rec := range result.Records {
		want, ok := classes[rec.Callsign]
		if !ok {
			t.Errorf("unexpected callsign %q", rec.Callsign)
			continue
		}
		if rec.LicenseClass != want {
			t.Errorf("callsign %s: license_class got %q, want %q", rec.Callsign, rec.LicenseClass, want)
		}
	}
}

// ── Status normalisation ──────────────────────────────────────────────────────

func TestParseANATELCSVData_StatusNormalisation(t *testing.T) {
	csvData := []byte(
		"indicativo;nome;uf;municipio;situacao\n" +
			"PY3ACT;Active;MG;Belo Horizonte;Ativo\n" +
			"PY3CAN;Cancelled;MG;Uberlandia;Cancelado\n" +
			"PY3EXP;Expired;MG;Contagem;Vencido\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
	}
	if len(result.Records) != 3 {
		t.Fatalf("records: got %d, want 3", len(result.Records))
	}

	for _, rec := range result.Records {
		switch rec.Callsign {
		case "PY3ACT":
			if rec.Status != "active" {
				t.Errorf("PY3ACT status: got %q, want active", rec.Status)
			}
		case "PY3CAN", "PY3EXP":
			if rec.Status != "expired" {
				t.Errorf("%s status: got %q, want expired", rec.Callsign, rec.Status)
			}
		}
	}
}

// ── Latin-1 encoding ──────────────────────────────────────────────────────────

func TestParseANATELCSVData_Latin1Encoding(t *testing.T) {
	input := "indicativo;nome;uf;municipio\nPP5RTL;Antônio Gonçalves;SC;Florianópolis\n"
	latin1, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(input))
	if err != nil {
		t.Fatalf("latin1 encode: %v", err)
	}

	result, err := callsign.ParseANATELCSVData(context.Background(), latin1)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "PP5RTL" {
		t.Errorf("callsign: got %q, want PP5RTL", rec.Callsign)
	}
	if rec.FullName != "Antônio Gonçalves" {
		t.Errorf("full_name: got %q, want Antônio Gonçalves", rec.FullName)
	}
	if rec.City != "Florianópolis" {
		t.Errorf("city: got %q, want Florianópolis", rec.City)
	}
}

// ── Comma delimiter (alternative format) ─────────────────────────────────────

func TestParseANATELCSVData_CommaDelimiter(t *testing.T) {
	csvData := []byte(
		"indicativo,nome,uf,municipio,categoria\n" +
			"PU4XYZ,Maria Oliveira,BA,Salvador,Classe B\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}
	if result.Records[0].Callsign != "PU4XYZ" {
		t.Errorf("callsign: got %q, want PU4XYZ", result.Records[0].Callsign)
	}
	if result.Records[0].LicenseClass != "class_b" {
		t.Errorf("license_class: got %q, want class_b", result.Records[0].LicenseClass)
	}
}

// ── Brazilian DD/MM/YYYY date format ─────────────────────────────────────────

func TestParseANATELCSVData_DateFormatDDMMYYYY(t *testing.T) {
	csvData := []byte(
		"indicativo;nome;uf;municipio;data_inicio_vigencia;data_fim_vigencia\n" +
			"PY7TUV;Carlos Pereira;PE;Recife;15/06/2017;15/06/2027\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
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

// ── Skip rows without a valid callsign ───────────────────────────────────────

func TestParseANATELCSVData_SkipsRowsWithoutValidCallsign(t *testing.T) {
	csvData := []byte(
		"indicativo;nome;uf;municipio\n" +
			";Sem Indicativo;SP;Campinas\n" + // empty callsign → skip
			"INVALIDO;Invalido;SP;Sorocaba\n" + // not a likely callsign → skip
			"PY2ZZZ;Válido;SP;Santos\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
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
	if result.Records[0].Callsign != "PY2ZZZ" {
		t.Errorf("callsign: got %q, want PY2ZZZ", result.Records[0].Callsign)
	}
}

// ── Reject HTML payloads ──────────────────────────────────────────────────────

func TestParseANATELCSVData_RejectsHTMLPayload(t *testing.T) {
	_, err := callsign.ParseANATELCSVData(context.Background(),
		[]byte("<!doctype html><html><body>Unauthorized</body></html>"))
	if err == nil {
		t.Fatal("expected error for HTML payload, got nil")
	}
}

// ── Missing callsign column returns descriptive error ─────────────────────────

func TestParseANATELCSVData_MissingCallsignColumn(t *testing.T) {
	csvData := []byte("nome;uf;municipio\nFulano;SP;Osasco\n")
	_, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err == nil {
		t.Fatal("expected error for missing callsign column, got nil")
	}
}

// ── Alternative column names ──────────────────────────────────────────────────

func TestParseANATELCSVData_AlternativeColumnNames(t *testing.T) {
	// Some ANATEL exports use "razao_social" instead of "nome", "classe" instead of "categoria", etc.
	csvData := []byte(
		"indicativo_de_chamada;razao_social;sigla_uf;cidade;classe;data_emissao;vencimento;status\n" +
			"ZV2TST;Rádio Clube Teste;RS;Porto Alegre;Classe A;2020-01-01;2030-01-01;vigente\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records: got %d, want 1", len(result.Records))
	}

	rec := result.Records[0]
	if rec.Callsign != "ZV2TST" {
		t.Errorf("callsign: got %q, want ZV2TST", rec.Callsign)
	}
	if rec.FullName != "Rádio Clube Teste" {
		t.Errorf("full_name: got %q, want Rádio Clube Teste", rec.FullName)
	}
	if rec.StateProvince != "RS" {
		t.Errorf("state_province: got %q, want RS", rec.StateProvince)
	}
	if rec.LicenseClass != "class_a" {
		t.Errorf("license_class: got %q, want class_a", rec.LicenseClass)
	}
	if rec.Status != "active" {
		t.Errorf("status: got %q, want active", rec.Status)
	}
}

// ── Multiple records round-trip ───────────────────────────────────────────────

func TestParseANATELCSVData_MultipleRecords(t *testing.T) {
	csvData := []byte(
		"indicativo;nome;uf;municipio;categoria;situacao\n" +
			"PY1AA;Alice;RJ;Rio de Janeiro;Classe A;Ativo\n" +
			"PY2BB;Bob;SP;São Paulo;Classe B;Ativo\n" +
			"PP3CC;Carol;MG;Belo Horizonte;Classe C;Cancelado\n" +
			"PU4DD;Dave;RS;Porto Alegre;Classe A;Ativo\n",
	)

	result, err := callsign.ParseANATELCSVData(context.Background(), csvData)
	if err != nil {
		t.Fatalf("ParseANATELCSVData: %v", err)
	}
	if result.Processed != 4 {
		t.Fatalf("processed: got %d, want 4", result.Processed)
	}
	if len(result.Records) != 4 {
		t.Fatalf("records: got %d, want 4", len(result.Records))
	}

	// All records should have country = Brazil.
	for _, rec := range result.Records {
		if rec.Country != "Brazil" {
			t.Errorf("record %s: country got %q, want Brazil", rec.Callsign, rec.Country)
		}
		if rec.Source != "anatel" {
			t.Errorf("record %s: source got %q, want anatel", rec.Callsign, rec.Source)
		}
	}
}
