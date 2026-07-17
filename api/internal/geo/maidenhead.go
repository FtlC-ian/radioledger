package geo

import (
	"math"
	"strings"
)

const (
	minLat = -90.0
	maxLat = 90.0
	minLon = -180.0
	maxLon = 180.0
)

// LatLonToGrid converts a WGS-84 coordinate to a 6-character Maidenhead grid.
//
// The returned grid uses the conventional mixed-case format:
//
//	AA00aa
func LatLonToGrid(lat, lon float64) string {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return ""
	}
	if lat < minLat || lat > maxLat || lon < minLon || lon > maxLon {
		return ""
	}

	adjLon := lon + 180.0
	adjLat := lat + 90.0

	fieldLon := int(adjLon / 20.0)
	fieldLat := int(adjLat / 10.0)
	if fieldLon > 17 {
		fieldLon = 17
	}
	if fieldLat > 17 {
		fieldLat = 17
	}

	remLon := math.Mod(adjLon, 20.0)
	remLat := math.Mod(adjLat, 10.0)
	squareLon := int(remLon / 2.0)
	squareLat := int(remLat / 1.0)
	if squareLon > 9 {
		squareLon = 9
	}
	if squareLat > 9 {
		squareLat = 9
	}

	remSubLon := math.Mod(remLon, 2.0)
	remSubLat := math.Mod(remLat, 1.0)
	subLon := int(remSubLon * 12.0)
	subLat := int(remSubLat * 24.0)
	if subLon > 23 {
		subLon = 23
	}
	if subLat > 23 {
		subLat = 23
	}

	grid := make([]byte, 6)
	grid[0] = byte('A' + fieldLon)
	grid[1] = byte('A' + fieldLat)
	grid[2] = byte('0' + squareLon)
	grid[3] = byte('0' + squareLat)
	grid[4] = byte('a' + subLon)
	grid[5] = byte('a' + subLat)
	return string(grid)
}

// GridToLatLon converts a 4- or 6-character Maidenhead grid to the center point
// of that square/subsquare.
//
// Invalid inputs return the zero value (0, 0).
func GridToLatLon(grid string) (lat, lon float64) {
	g := strings.TrimSpace(grid)
	if len(g) != 4 && len(g) != 6 {
		return 0, 0
	}
	g = strings.ToUpper(g)

	if g[0] < 'A' || g[0] > 'R' || g[1] < 'A' || g[1] > 'R' {
		return 0, 0
	}
	if g[2] < '0' || g[2] > '9' || g[3] < '0' || g[3] > '9' {
		return 0, 0
	}
	if len(g) == 6 {
		if g[4] < 'A' || g[4] > 'X' || g[5] < 'A' || g[5] > 'X' {
			return 0, 0
		}
	}

	lon = float64(g[0]-'A') * 20.0
	lat = float64(g[1]-'A') * 10.0

	lon += float64(g[2]-'0') * 2.0
	lat += float64(g[3]-'0') * 1.0

	if len(g) == 6 {
		lon += float64(g[4]-'A') * (2.0 / 24.0)
		lat += float64(g[5]-'A') * (1.0 / 24.0)
		lon += 1.0 / 24.0
		lat += 0.5 / 24.0
	} else {
		lon += 1.0
		lat += 0.5
	}

	lon -= 180.0
	lat -= 90.0
	return lat, lon
}
