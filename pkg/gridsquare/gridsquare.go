// Package gridsquare implements Maidenhead Locator System (grid square) utilities.
//
// The Maidenhead Locator System divides the earth into a hierarchy of cells:
//
//   - Field:     2 letters (A-R), 20° longitude × 10° latitude
//   - Square:    2 digits  (0-9),  2° longitude ×  1° latitude
//   - Subsquare: 2 letters (A-X),  5′ longitude × 2.5′ latitude
//
// Four-character grid squares (field + square) are used for most ham radio
// logging and awards. Six-character grid squares (field + square + subsquare)
// are used for VHF/UHF operation where precision matters.
//
// This package is a pure utility library with no database dependency.
// It mirrors the database-side maidenhead_to_point() PostGIS function.
package gridsquare

import (
	"fmt"
	"math"
	"strings"
)

// LatLng represents a WGS-84 geographic coordinate.
type LatLng struct {
	// Lat is the latitude in decimal degrees, -90 to +90.
	Lat float64
	// Lng is the longitude in decimal degrees, -180 to +180.
	Lng float64
}

// FromLatLng converts a WGS-84 coordinate to a Maidenhead grid square.
// precision must be 4 or 6. Any other value returns an error.
//
// lat must be in [-90, 90]; lng must be in [-180, 180].
func FromLatLng(lat, lng float64, precision int) (string, error) {
	if precision != 4 && precision != 6 {
		return "", fmt.Errorf("gridsquare: precision must be 4 or 6, got %d", precision)
	}
	if lat < -90 || lat > 90 {
		return "", fmt.Errorf("gridsquare: latitude %v out of range [-90, 90]", lat)
	}
	if lng < -180 || lng > 180 {
		return "", fmt.Errorf("gridsquare: longitude %v out of range [-180, 180]", lng)
	}

	// Shift to positive-only coordinate space.
	adjLon := lng + 180.0 // 0..360
	adjLat := lat + 90.0  // 0..180

	// Field letters: each field is 20° lon × 10° lat.
	fieldLon := int(adjLon / 20.0) // 0..17
	fieldLat := int(adjLat / 10.0) // 0..17
	// Clamp to handle exactly +180 lon / +90 lat edge cases.
	if fieldLon > 17 {
		fieldLon = 17
	}
	if fieldLat > 17 {
		fieldLat = 17
	}

	// Square digits: each square is 2° lon × 1° lat.
	remLon := math.Mod(adjLon, 20.0) // 0..20
	remLat := math.Mod(adjLat, 10.0) // 0..10
	squareLon := int(remLon / 2.0)   // 0..9
	squareLat := int(remLat / 1.0)   // 0..9
	if squareLon > 9 {
		squareLon = 9
	}
	if squareLat > 9 {
		squareLat = 9
	}

	grid := fmt.Sprintf("%c%c%d%d",
		rune('A'+fieldLon),
		rune('A'+fieldLat),
		squareLon,
		squareLat,
	)

	if precision == 6 {
		// Subsquare letters: each subsquare is (2/24)° lon × (1/24)° lat.
		rem2Lon := math.Mod(remLon, 2.0)   // 0..2
		rem2Lat := math.Mod(remLat, 1.0)   // 0..1
		subLon := int(rem2Lon * 12.0)      // 0..23
		subLat := int(rem2Lat * 24.0)      // 0..23
		if subLon > 23 {
			subLon = 23
		}
		if subLat > 23 {
			subLat = 23
		}
		grid += fmt.Sprintf("%c%c",
			rune('A'+subLon),
			rune('A'+subLat),
		)
	}

	return grid, nil
}

// ToLatLng converts a Maidenhead grid square to the WGS-84 coordinate of its center.
// Accepts 4-character (field+square) or 6-character (field+square+subsquare) grids.
//
// The input is case-insensitive. Returns an error if the format is invalid.
func ToLatLng(grid string) (LatLng, error) {
	if err := Validate(grid); err != nil {
		return LatLng{}, err
	}
	g := strings.ToUpper(grid)

	// Field: A-R
	lon := float64(g[0]-'A') * 20.0 // 0..340
	lat := float64(g[1]-'A') * 10.0 // 0..170

	// Square: 0-9
	lon += float64(g[2]-'0') * 2.0 // add 0..18
	lat += float64(g[3]-'0') * 1.0 // add 0..9

	if len(g) >= 6 {
		// Subsquare: A-X, each 2/24° lon × 1/24° lat.
		lon += float64(g[4]-'A') * (2.0 / 24.0)
		lat += float64(g[5]-'A') * (1.0 / 24.0)
		// Center of subsquare.
		lon += 1.0 / 24.0
		lat += 0.5 / 24.0
	} else {
		// Center of square.
		lon += 1.0
		lat += 0.5
	}

	// Shift back to signed coordinate space.
	return LatLng{
		Lat: lat - 90.0,
		Lng: lon - 180.0,
	}, nil
}

// Validate reports whether grid is a valid Maidenhead locator string.
// Accepts 4-character (field+square) and 6-character (field+square+subsquare) formats.
// The check is case-insensitive.
//
// Valid examples: "EM10", "EM10aa", "JO62LJ", "FN31pr"
func Validate(grid string) error {
	g := strings.ToUpper(grid)
	switch len(g) {
	case 4, 6:
		// OK — continue validation below.
	default:
		return fmt.Errorf("gridsquare: %q has invalid length %d (must be 4 or 6)", grid, len(grid))
	}

	// Field pair: A-R
	if g[0] < 'A' || g[0] > 'R' {
		return fmt.Errorf("gridsquare: %q field longitude letter %q out of range A-R", grid, string(g[0]))
	}
	if g[1] < 'A' || g[1] > 'R' {
		return fmt.Errorf("gridsquare: %q field latitude letter %q out of range A-R", grid, string(g[1]))
	}
	// Square pair: 0-9
	if g[2] < '0' || g[2] > '9' {
		return fmt.Errorf("gridsquare: %q square longitude digit %q is not 0-9", grid, string(g[2]))
	}
	if g[3] < '0' || g[3] > '9' {
		return fmt.Errorf("gridsquare: %q square latitude digit %q is not 0-9", grid, string(g[3]))
	}
	if len(g) == 6 {
		// Subsquare pair: A-X
		if g[4] < 'A' || g[4] > 'X' {
			return fmt.Errorf("gridsquare: %q subsquare longitude letter %q out of range A-X", grid, string(g[4]))
		}
		if g[5] < 'A' || g[5] > 'X' {
			return fmt.Errorf("gridsquare: %q subsquare latitude letter %q out of range A-X", grid, string(g[5]))
		}
	}
	return nil
}

// Distance returns the great-circle distance in kilometers between the centers
// of two Maidenhead grid squares. Both grids are validated; an error is returned
// if either is invalid.
//
// Uses the Haversine formula (WGS-84 mean Earth radius 6371 km).
func Distance(a, b string) (float64, error) {
	ll1, err := ToLatLng(a)
	if err != nil {
		return 0, fmt.Errorf("gridsquare: first grid: %w", err)
	}
	ll2, err := ToLatLng(b)
	if err != nil {
		return 0, fmt.Errorf("gridsquare: second grid: %w", err)
	}
	return haversineKm(ll1, ll2), nil
}

const earthRadiusKm = 6371.0

// haversineKm computes the great-circle distance in km between two WGS-84 points.
// Uses the Haversine formula for numerical stability at small distances.
func haversineKm(a, b LatLng) float64 {
	dLat := deg2rad(b.Lat - a.Lat)
	dLng := deg2rad(b.Lng - a.Lng)
	lat1 := deg2rad(a.Lat)
	lat2 := deg2rad(b.Lat)

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	return earthRadiusKm * c
}

func deg2rad(d float64) float64 {
	return d * math.Pi / 180.0
}
