package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	censusBaseURL = "https://geocoding.geo.census.gov/geocoder/addresses/onelineaddress"
	censusClient  = &http.Client{Timeout: 5 * time.Second}
)

type censusResponse struct {
	Result struct {
		AddressMatches []struct {
			Coordinates struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			} `json:"coordinates"`
		} `json:"addressMatches"`
	} `json:"result"`
}

// GeocodeAddress resolves a US street address to latitude/longitude using the
// US Census Geocoder. It requires at minimum a street address — city-only or
// zip-only queries will return an error; those cases should use
// GeocodeFromZipCentroid instead.
func GeocodeAddress(ctx context.Context, street, city, state, zip string) (lat, lon float64, err error) {
	return geocodeCensus(ctx, street, city, state, zip)
}

func geocodeCensus(ctx context.Context, street, city, state, zip string) (lat, lon float64, err error) {
	address := formatCensusAddress(street, city, state, zip)
	if address == "" {
		return 0, 0, fmt.Errorf("address is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	u, err := url.Parse(censusBaseURL)
	if err != nil {
		return 0, 0, fmt.Errorf("parse census url: %w", err)
	}

	q := u.Query()
	q.Set("address", address)
	q.Set("benchmark", "Public_AR_Current")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, 0, fmt.Errorf("build census request: %w", err)
	}

	resp, err := censusClient.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("census geocoder request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close census response body: %w", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("census geocoder returned %s", resp.Status)
	}

	var payload censusResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, 0, fmt.Errorf("decode census response: %w", err)
	}
	if len(payload.Result.AddressMatches) == 0 {
		return 0, 0, fmt.Errorf("no census address match")
	}

	match := payload.Result.AddressMatches[0].Coordinates
	return match.Y, match.X, nil
}

// GeocodeFromZipCentroid looks up the zip centroid from the local database.
// It returns the latitude and longitude for the given zip code using the
// Census ZCTA5 centroid data loaded by ZCTARefreshWorker.
func GeocodeFromZipCentroid(ctx context.Context, pool *pgxpool.Pool, zip string) (lat, lon float64, err error) {
	zip = strings.TrimSpace(zip)
	if zip == "" {
		return 0, 0, fmt.Errorf("zip code is required")
	}

	// Normalize to 5-digit ZIP (strip ZIP+4 suffix if present).
	if len(zip) > 5 {
		zip = zip[:5]
	}

	err = pool.QueryRow(ctx,
		`SELECT latitude, longitude FROM zip_centroids WHERE zip_code = $1`,
		zip,
	).Scan(&lat, &lon)
	if err != nil {
		return 0, 0, fmt.Errorf("zip centroid lookup for %q: %w", zip, err)
	}
	return lat, lon, nil
}

func formatCensusAddress(street, city, state, zip string) string {
	street = strings.TrimSpace(street)
	city = strings.TrimSpace(city)
	state = strings.TrimSpace(state)
	zip = strings.TrimSpace(zip)

	location := strings.Trim(strings.Join([]string{city, stateOrZip(state, zip)}, ", "), ", ")
	if street == "" || isPOBox(street) {
		return location
	}
	if location == "" {
		return street
	}
	return street + ", " + location
}

func stateOrZip(state, zip string) string {
	state = strings.TrimSpace(state)
	zip = strings.TrimSpace(zip)
	switch {
	case state != "" && zip != "":
		return state + " " + zip
	case state != "":
		return state
	default:
		return zip
	}
}

// IsPOBox reports whether the street string looks like a PO Box address.
func IsPOBox(street string) bool {
	return isPOBox(street)
}

func isPOBox(street string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(street))
	normalized = strings.ReplaceAll(normalized, ".", "")
	normalized = strings.ReplaceAll(normalized, "  ", " ")
	return strings.Contains(normalized, "PO BOX") ||
		strings.Contains(normalized, "P O BOX") ||
		strings.Contains(normalized, "POST OFFICE BOX")
}
