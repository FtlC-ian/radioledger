package gridsquare_test

import (
	"math"
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/gridsquare"
)

// TestFromLatLng verifies lat/lng → grid conversion using known reference points.
func TestFromLatLng(t *testing.T) {
	t.Parallel()

	// Grid square bases (lower-left corner of the square, before center offset):
	//   EM: E=field lon 4 → lon base=-100; M=field lat 12 → lat base=30
	//   EM10: square 1 → lon=-100+2=-98; square 0 → lat=30+0=30
	//   EM10 center: lon=-98+1=-97, lat=30+0.5=30.5
	//
	//   FN: F=field lon 5 → lon base=-80; N=field lat 13 → lat base=40
	//   FN31: square 3 → lon=-80+6=-74; square 1 → lat=40+1=41
	//   FN31 center: lon=-74+1=-73, lat=41+0.5=41.5
	//
	//   JO: J=field lon 9 → lon base=0; O=field lat 14 → lat base=50
	//   JO62: square 6 → lon=0+12=12; square 2 → lat=50+2=52
	//   JO62 center: lon=12+1=13, lat=52+0.5=52.5

	cases := []struct {
		name      string
		lat, lng  float64
		precision int
		want      string
	}{
		{name: "EM10_center_4char", lat: 30.5, lng: -97.0, precision: 4, want: "EM10"},
		{name: "FN31_center_4char", lat: 41.5, lng: -73.0, precision: 4, want: "FN31"},
		{name: "JO62_center_4char", lat: 52.5, lng: 13.0, precision: 4, want: "JO62"},
		// DM: D=field lon 3 → lon base=-120; M=field lat 12 → lat base=30
		// DM79: square 7 → lon=-120+14=-106; square 9 → lat=30+9=39
		// DM79 center: lon=-106+1=-105, lat=39+0.5=39.5
		{name: "DM79_center_4char", lat: 39.5, lng: -105.0, precision: 4, want: "DM79"},
		// EM10AA center: EM10 base lon=-98, lat=30; sub A=0 → offset=(1/24, 0.5/24)
		{name: "EM10AA_center_6char", lat: 30.0 + 0.5/24.0, lng: -98.0 + 1.0/24.0, precision: 6, want: "EM10AA"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := gridsquare.FromLatLng(tc.lat, tc.lng, tc.precision)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("FromLatLng(%v, %v, %d) = %q, want %q", tc.lat, tc.lng, tc.precision, got, tc.want)
			}
		})
	}
}

// TestToLatLng verifies grid → lat/lng conversion.
func TestToLatLng(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		grid     string
		wantLat  float64
		wantLng  float64
		tolerDeg float64
	}{
		{name: "EM10", grid: "EM10", wantLat: 30.5, wantLng: -97.0, tolerDeg: 0.001},
		{name: "EM10_lower", grid: "em10", wantLat: 30.5, wantLng: -97.0, tolerDeg: 0.001},
		{name: "FN31", grid: "FN31", wantLat: 41.5, wantLng: -73.0, tolerDeg: 0.001},
		{name: "JO62", grid: "JO62", wantLat: 52.5, wantLng: 13.0, tolerDeg: 0.001},
		{name: "DM79", grid: "DM79", wantLat: 39.5, wantLng: -105.0, tolerDeg: 0.001},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := gridsquare.ToLatLng(tc.grid)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got.Lat-tc.wantLat) > tc.tolerDeg {
				t.Errorf("ToLatLng(%q).Lat = %v, want %v (±%v)", tc.grid, got.Lat, tc.wantLat, tc.tolerDeg)
			}
			if math.Abs(got.Lng-tc.wantLng) > tc.tolerDeg {
				t.Errorf("ToLatLng(%q).Lng = %v, want %v (±%v)", tc.grid, got.Lng, tc.wantLng, tc.tolerDeg)
			}
		})
	}
}

// TestRoundTrip verifies that lat/lng → grid → lat/lng returns close to the original.
func TestRoundTrip(t *testing.T) {
	t.Parallel()

	points := []gridsquare.LatLng{
		{Lat: 30.5, Lng: -97.0},   // EM10 center
		{Lat: 51.5, Lng: -0.12},   // London
		{Lat: 35.6, Lng: 139.7},   // Tokyo
		{Lat: -33.9, Lng: 151.2},  // Sydney
		{Lat: 0.0, Lng: 0.0},      // Null island
		{Lat: 89.0, Lng: 0.0},     // Near north pole
		{Lat: -89.0, Lng: 0.0},    // Near south pole
		{Lat: 0.0, Lng: 179.9},    // Near dateline east
		{Lat: 0.0, Lng: -179.9},   // Near dateline west
	}

	for _, p := range points {
		grid4, err := gridsquare.FromLatLng(p.Lat, p.Lng, 4)
		if err != nil {
			t.Fatalf("FromLatLng(%v,%v,4): %v", p.Lat, p.Lng, err)
		}
		back4, err := gridsquare.ToLatLng(grid4)
		if err != nil {
			t.Fatalf("ToLatLng(%q): %v", grid4, err)
		}
		// 4-char grid cell: 2° lon × 1° lat; center within 1°×0.5° of any point in that cell.
		if math.Abs(back4.Lat-p.Lat) > 1.0 {
			t.Errorf("4-char round-trip lat drift too large: %v → %s → %v", p.Lat, grid4, back4.Lat)
		}
		if math.Abs(back4.Lng-p.Lng) > 2.0 {
			t.Errorf("4-char round-trip lng drift too large: %v → %s → %v", p.Lng, grid4, back4.Lng)
		}

		grid6, err := gridsquare.FromLatLng(p.Lat, p.Lng, 6)
		if err != nil {
			t.Fatalf("FromLatLng(%v,%v,6): %v", p.Lat, p.Lng, err)
		}
		back6, err := gridsquare.ToLatLng(grid6)
		if err != nil {
			t.Fatalf("ToLatLng(%q): %v", grid6, err)
		}
		// 6-char subsquare: (2/24)°≈5′ lon × (1/24)°≈2.5′ lat
		const tolerDeg6 = 0.1 // well within half-subsquare
		if math.Abs(back6.Lat-p.Lat) > tolerDeg6 {
			t.Errorf("6-char round-trip lat drift too large: %v → %s → %v", p.Lat, grid6, back6.Lat)
		}
		if math.Abs(back6.Lng-p.Lng) > tolerDeg6 {
			t.Errorf("6-char round-trip lng drift too large: %v → %s → %v", p.Lng, grid6, back6.Lng)
		}
	}
}

// TestValidate checks valid and invalid grid square strings.
func TestValidate(t *testing.T) {
	t.Parallel()

	valid := []string{
		"AA00", "RR99", "EM10", "FN31", "JO62", "DM79",
		"EM10aa", "FN31pr", "JO62LJ", "AA00AA", "RR99XX",
	}
	for _, g := range valid {
		if err := gridsquare.Validate(g); err != nil {
			t.Errorf("Validate(%q) should be valid, got: %v", g, err)
		}
	}

	invalid := []string{
		"",        // empty
		"A",       // too short
		"AA0",     // 3 chars
		"AA000",   // 5 chars
		"AA0000A", // 7 chars
		"SA00",    // S is out of range A-R
		"A900",    // 9 is not a letter
		"AA0Z",    // Z is not a digit
		"AA00AY",  // Y is out of range A-X
	}
	for _, g := range invalid {
		if err := gridsquare.Validate(g); err == nil {
			t.Errorf("Validate(%q) should be invalid, got nil", g)
		}
	}
}

// TestDistance checks great-circle distances between known grid squares.
func TestDistance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		a, b          string
		wantApproxKm  float64
		tolerKm       float64
	}{
		{
			// EM10 center (lat=30.5, lng=-97) to FN31 center (lat=41.5, lng=-73)
			// Haversine ≈ 2469 km
			name:         "EM10_to_FN31",
			a:            "EM10",
			b:            "FN31",
			wantApproxKm: 2469,
			tolerKm:      100,
		},
		{
			// Same grid → distance ≈ 0
			name:         "same_grid",
			a:            "JO62",
			b:            "JO62",
			wantApproxKm: 0,
			tolerKm:      1,
		},
		{
			// EM10 (lat=30.5, lng=-97) to JO62 (lat=52.5, lng=13) — roughly 8900 km
			name:         "EM10_to_JO62",
			a:            "EM10",
			b:            "JO62",
			wantApproxKm: 8900,
			tolerKm:      500,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := gridsquare.Distance(tc.a, tc.b)
			if err != nil {
				t.Fatalf("Distance(%q, %q): %v", tc.a, tc.b, err)
			}
			if math.Abs(got-tc.wantApproxKm) > tc.tolerKm {
				t.Errorf("Distance(%q, %q) = %.1f km, want %.1f ± %.1f",
					tc.a, tc.b, got, tc.wantApproxKm, tc.tolerKm)
			}
		})
	}
}

// TestInvalidInputs covers error cases for FromLatLng and Distance.
func TestInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := gridsquare.FromLatLng(100, 0, 4); err == nil {
		t.Error("expected error for lat=100")
	}
	if _, err := gridsquare.FromLatLng(0, 200, 4); err == nil {
		t.Error("expected error for lng=200")
	}
	if _, err := gridsquare.FromLatLng(0, 0, 5); err == nil {
		t.Error("expected error for precision=5")
	}
	if _, err := gridsquare.Distance("EM10", "INVALID"); err == nil {
		t.Error("expected error for invalid second grid")
	}
	if _, err := gridsquare.Distance("ZZZZ", "EM10"); err == nil {
		t.Error("expected error for invalid first grid")
	}
}
