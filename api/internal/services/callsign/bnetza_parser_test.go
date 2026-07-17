package callsign

import "testing"

func TestParseBNetzAText_BasicRecord(t *testing.T) {
	text := `
1
Liste der personengebundenen Rufzeichen gemäß § 3 Abs. 1 und Abs. 3 Nr. 1 des
Amateurfunkgesetzes
DA1AA, A, Norman Czora; Leipziger Str. 212, 38124 Braunschweig
`

	result := parseBNetzAText(text)
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
	if rec.Callsign != "DA1AA" {
		t.Errorf("callsign: got %q, want DA1AA", rec.Callsign)
	}
	if rec.FullName != "Norman Czora" {
		t.Errorf("full_name: got %q, want Norman Czora", rec.FullName)
	}
	if rec.PostalCode != "38124" {
		t.Errorf("postal_code: got %q, want 38124", rec.PostalCode)
	}
	if rec.City != "Braunschweig" {
		t.Errorf("city: got %q, want Braunschweig", rec.City)
	}
	if rec.Country != "Germany" {
		t.Errorf("country: got %q, want Germany", rec.Country)
	}
	if rec.Source != "bnetza" {
		t.Errorf("source: got %q, want bnetza", rec.Source)
	}
	if rec.LicenseClass != "a" {
		t.Errorf("license_class: got %q, want a", rec.LicenseClass)
	}
}

func TestParseBNetzAText_MultilineRecordAndUmlaut(t *testing.T) {
	text := `
DA1AP, A, Manuel Reckenbeil; Hotzelgasse
18 a, 36456 Barchfeld-Immelborn,
Diakonissenweg 1, 36456 Barchfeld-Immelborn
DA1LG, A, Lennart Götz
`

	result := parseBNetzAText(text)
	if result.Processed != 2 {
		t.Fatalf("processed: got %d, want 2", result.Processed)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records: got %d, want 2", len(result.Records))
	}

	first := result.Records[0]
	if first.Callsign != "DA1AP" {
		t.Fatalf("first callsign: got %q, want DA1AP", first.Callsign)
	}
	if first.PostalCode != "36456" {
		t.Errorf("first postal_code: got %q, want 36456", first.PostalCode)
	}
	if first.City != "Barchfeld-Immelborn" {
		t.Errorf("first city: got %q, want Barchfeld-Immelborn", first.City)
	}

	second := result.Records[1]
	if second.Callsign != "DA1LG" {
		t.Fatalf("second callsign: got %q, want DA1LG", second.Callsign)
	}
	if second.FullName != "Lennart Götz" {
		t.Errorf("second full_name: got %q, want Lennart Götz", second.FullName)
	}
}

func TestParseBNetzAText_IgnoresAddressContinuationFalseStart(t *testing.T) {
	text := `
DF6ZW, A, Stefan Burkhardt; Unterer
Mittelweg 30, 61352 Bad Homburg, FH der
DBP, 64807 Dieburg
DF6ZX, A, Alfred Braunbeck; Kirchgasse 1, 63796 Kahl
`

	result := parseBNetzAText(text)
	if result.Processed != 2 {
		t.Fatalf("processed: got %d, want 2", result.Processed)
	}
	if len(result.Records) != 2 {
		t.Fatalf("records: got %d, want 2", len(result.Records))
	}
	if result.Records[0].Callsign != "DF6ZW" {
		t.Fatalf("first callsign: got %q, want DF6ZW", result.Records[0].Callsign)
	}
	if result.Records[1].Callsign != "DF6ZX" {
		t.Fatalf("second callsign: got %q, want DF6ZX", result.Records[1].Callsign)
	}
}
