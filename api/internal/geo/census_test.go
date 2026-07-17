package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGeocodeAddress_UsesStreetAddress(t *testing.T) {
	oldURL := censusBaseURL
	oldClient := censusClient
	defer func() {
		censusBaseURL = oldURL
		censusClient = oldClient
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("address"); got != "225 Main St, Newington, CT 06111" {
			t.Fatalf("address query: got %q", got)
		}
		_, _ = w.Write([]byte(`{"result":{"addressMatches":[{"coordinates":{"x":-72.7272,"y":41.7148}}]}}`))
	}))
	defer server.Close()

	censusBaseURL = server.URL
	censusClient = server.Client()

	lat, lon, err := GeocodeAddress(context.Background(), "225 Main St", "Newington", "CT", "06111")
	if err != nil {
		t.Fatalf("GeocodeAddress error: %v", err)
	}
	if lat != 41.7148 || lon != -72.7272 {
		t.Fatalf("GeocodeAddress: got (%f, %f)", lat, lon)
	}
}

func TestGeocodeAddress_ReturnsErrorWhenNoMatch(t *testing.T) {
	oldURL := censusBaseURL
	oldClient := censusClient
	defer func() {
		censusBaseURL = oldURL
		censusClient = oldClient
	}()

	// Census server returns no matches (e.g. for a PO Box / city-only query).
	censusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"addressMatches":[]}}`))
	}))
	defer censusServer.Close()

	censusBaseURL = censusServer.URL
	censusClient = censusServer.Client()

	// GeocodeAddress no longer falls back to Nominatim — it returns an error.
	_, _, err := GeocodeAddress(context.Background(), "PO Box 123", "Maumelle", "AR", "72113")
	if err == nil {
		t.Fatal("expected error when census returns no match, got nil")
	}
}

func TestGeocodeAddress_EmptyAddressReturnsError(t *testing.T) {
	// With no usable address components, census.go should error without hitting the network.
	_, _, err := GeocodeAddress(context.Background(), "", "", "", "")
	if err == nil {
		t.Fatal("expected error for empty address, got nil")
	}
}

// TestGeocodeFromZipCentroid_NotInDB verifies that GeocodeFromZipCentroid returns
// an error when the zip code is not in the local table.
//
// This is a unit test that uses a nil pool — the function only hits the DB for
// valid zip codes. We validate the empty-zip guard here without a real DB.
func TestGeocodeFromZipCentroid_EmptyZipReturnsError(t *testing.T) {
	_, _, err := GeocodeFromZipCentroid(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for empty zip, got nil")
	}
}

// TestFormatCensusAddress exercises the address formatter used by the Census
// geocoder.  These are pure unit tests with no network I/O.
func TestFormatCensusAddress(t *testing.T) {
	tests := []struct {
		street, city, state, zip string
		want                     string
	}{
		{"225 Main St", "Newington", "CT", "06111", "225 Main St, Newington, CT 06111"},
		{"", "Newington", "CT", "06111", "Newington, CT 06111"},
		{"P.O. Box 123", "Maumelle", "AR", "72113", "Maumelle, AR 72113"},
		{"", "Chino Hills", "CA", "", "Chino Hills, CA"},
		{"", "", "CA", "91709", "CA 91709"},
		{"", "", "", "91709", "91709"},
		{"123 Oak Ave", "", "", "", "123 Oak Ave"},
	}
	for _, tc := range tests {
		got := formatCensusAddress(tc.street, tc.city, tc.state, tc.zip)
		if got != tc.want {
			t.Errorf("formatCensusAddress(%q,%q,%q,%q) = %q; want %q",
				tc.street, tc.city, tc.state, tc.zip, got, tc.want)
		}
	}
}

// TestIsPOBox checks the PO Box detection heuristic.
func TestIsPOBox(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"P.O. Box 123", true},
		{"PO BOX 456", true},
		{"Post Office Box 7", true},
		{"225 Main St", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isPOBox(tc.input); got != tc.want {
			t.Errorf("isPOBox(%q) = %v; want %v", tc.input, got, tc.want)
		}
	}
}
