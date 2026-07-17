package handler

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

type BandModeVisibilityHandler struct {
	pool *pgxpool.Pool
}

func NewBandModeVisibilityHandler(pool *pgxpool.Pool) *BandModeVisibilityHandler {
	return &BandModeVisibilityHandler{pool: pool}
}

type bandVisibilityItem struct {
	Name             string `json:"name"`
	Label            string `json:"label"`
	BandGroup        string `json:"band_group,omitempty"`
	IsCommon         bool   `json:"is_common"`
	IsDefaultVisible bool   `json:"is_default_visible"`
	IsVisible        bool   `json:"is_visible"`
	SortOrder        int    `json:"sort_order"`
}

type modeVisibilityItem struct {
	Name             string `json:"name"`
	Label            string `json:"label"`
	Category         string `json:"category,omitempty"`
	IsPopular        bool   `json:"is_popular"`
	IsDefaultVisible bool   `json:"is_default_visible"`
	IsVisible        bool   `json:"is_visible"`
	SortOrder        int    `json:"sort_order"`
}

type bandRow struct {
	Name             string
	BandGroup        *string
	IsCommon         bool
	SortOrder        int32
	IsDefaultVisible bool
}

type modeRow struct {
	Name       string
	Category   *string
	IsPopular  bool
	SortOrder  int32
}

func (h *BandModeVisibilityHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := requestUserID(r)
	if err != nil {
		writeFailure(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	tx, err := beginTenantTx(r.Context(), h.pool, userID)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	queries := sqlc.New(tx)
	prefsRow, err := queries.GetUserPreferences(r.Context(), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeFailure(w, http.StatusNotFound, "not found", "user not found")
		return
	}
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	prefsMap := decodePreferencesMap(prefsRow.Preferences)
	ituRegion, regionSource, err := detectUserITURegion(r.Context(), tx, userID, prefsMap)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	bands, err := loadBandVisibilityRows(r.Context(), tx, ituRegion)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "failed to load band visibility")
		return
	}
	modes, err := loadModeVisibilityRows(r.Context(), tx)
	if err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "failed to load mode visibility")
		return
	}

	bandNames := make([]string, 0, len(bands))
	defaultVisibleBands := make([]string, 0, len(bands))
	for _, band := range bands {
		bandNames = append(bandNames, band.Name)
		if band.IsDefaultVisible {
			defaultVisibleBands = append(defaultVisibleBands, band.Name)
		}
	}

	modeNames := make([]string, 0, len(modes))
	defaultVisibleModes := make([]string, 0, len(modes))
	for _, mode := range modes {
		modeNames = append(modeNames, mode.Name)
		if mode.IsPopular {
			defaultVisibleModes = append(defaultVisibleModes, mode.Name)
		}
	}

	explicitBands := stringSlicePreferenceValue(prefsMap, "visible_bands")
	explicitModes := stringSlicePreferenceValue(prefsMap, "visible_modes")
	hasExplicitBands := explicitBands != nil
	hasExplicitModes := explicitModes != nil
	effectiveVisibleBands := filterKnownValues(explicitBands, bandNames)
	effectiveVisibleModes := filterKnownValues(explicitModes, modeNames)

	if !hasExplicitBands || len(effectiveVisibleBands) == 0 {
		effectiveVisibleBands = defaultVisibleBands
	}
	if !hasExplicitModes || len(effectiveVisibleModes) == 0 {
		effectiveVisibleModes = defaultVisibleModes
	}

	visibleBandSet := sliceToSet(effectiveVisibleBands)
	visibleModeSet := sliceToSet(effectiveVisibleModes)

	bandItems := make([]bandVisibilityItem, 0, len(bands))
	for _, band := range bands {
		bandItems = append(bandItems, bandVisibilityItem{
			Name:             band.Name,
			Label:            band.Name,
			BandGroup:        nullableString(band.BandGroup),
			IsCommon:         band.IsCommon,
			IsDefaultVisible: band.IsDefaultVisible,
			IsVisible:        visibleBandSet[band.Name],
			SortOrder:        int(band.SortOrder),
		})
	}

	modeItems := make([]modeVisibilityItem, 0, len(modes))
	for _, mode := range modes {
		modeItems = append(modeItems, modeVisibilityItem{
			Name:             mode.Name,
			Label:            mode.Name,
			Category:         nullableString(mode.Category),
			IsPopular:        mode.IsPopular,
			IsDefaultVisible: mode.IsPopular,
			IsVisible:        visibleModeSet[mode.Name],
			SortOrder:        int(mode.SortOrder),
		})
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeFailure(w, http.StatusInternalServerError, "database error", "internal error")
		return
	}

	writeSuccess(w, http.StatusOK, "band and mode visibility retrieved", map[string]any{
		"itu_region":    ituRegion,
		"region_source": regionSource,
		"bands":         bandItems,
		"modes":         modeItems,
		"visible_bands": effectiveVisibleBands,
		"visible_modes": effectiveVisibleModes,
	})
}

func detectUserITURegion(ctx context.Context, tx pgx.Tx, userID int64, prefs map[string]any) (*int, string, error) {
	if region, ok := intValue(prefs, "itu_region"); ok {
		if region >= 1 && region <= 3 {
			return &region, "explicit", nil
		}
	}

	var callsign string
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(callsign, '')
		FROM users
		WHERE id = $1
		  AND deleted_at IS NULL
	`, userID).Scan(&callsign); err != nil {
		return nil, "unknown", err
	}

	if strings.TrimSpace(callsign) == "" {
		return nil, "unknown", nil
	}

	var continent string
	err := tx.QueryRow(ctx, `
		SELECT d.continent
		FROM dxcc_prefixes p
		JOIN dxcc_entities d ON d.entity_id = p.entity_id
		WHERE UPPER(BTRIM($1::text)) LIKE p.prefix || '%'
		ORDER BY LENGTH(p.prefix) DESC, p.prefix ASC
		LIMIT 1
	`, strings.ToUpper(strings.TrimSpace(callsign))).Scan(&continent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "unknown", nil
	}
	if err != nil {
		return nil, "unknown", err
	}

	region := continentToITURegion(continent)
	if region == nil {
		return nil, "unknown", nil
	}
	return region, "callsign_prefix", nil
}

func continentToITURegion(continent string) *int {
	switch strings.ToUpper(strings.TrimSpace(continent)) {
	case "EU", "AF":
		region := 1
		return &region
	case "NA", "SA":
		region := 2
		return &region
	case "AS", "OC", "AN":
		region := 3
		return &region
	default:
		return nil
	}
}

func loadBandVisibilityRows(ctx context.Context, tx pgx.Tx, ituRegion *int) ([]bandRow, error) {
	const sql = `
		SELECT
			b.name,
			b.band_group,
			b.is_common,
			b.sort_order,
			CASE
				WHEN $1::integer IS NULL THEN b.is_common
				ELSE COALESCE(bra.is_default_visible, b.is_common)
			END AS is_default_visible
		FROM bands b
		LEFT JOIN band_region_allocations bra
			ON bra.band_name = b.name
			AND bra.itu_region = $1::integer
		ORDER BY b.sort_order ASC, b.lower_freq ASC, b.name ASC
	`

	rows, err := tx.Query(ctx, sql, ituRegion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []bandRow{}
	for rows.Next() {
		var row bandRow
		if err := rows.Scan(&row.Name, &row.BandGroup, &row.IsCommon, &row.SortOrder, &row.IsDefaultVisible); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func loadModeVisibilityRows(ctx context.Context, tx pgx.Tx) ([]modeRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT name, category, is_popular, sort_order
		FROM modes
		ORDER BY sort_order ASC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []modeRow{}
	for rows.Next() {
		var row modeRow
		if err := rows.Scan(&row.Name, &row.Category, &row.IsPopular, &row.SortOrder); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

func filterKnownValues(values []string, known []string) []string {
	if len(values) == 0 || len(known) == 0 {
		return nil
	}

	knownSet := sliceToSet(known)
	filtered := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !knownSet[value] || seen[value] {
			continue
		}
		filtered = append(filtered, value)
		seen[value] = true
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		return indexOf(known, filtered[i]) < indexOf(known, filtered[j])
	})

	return filtered
}

func sliceToSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func nullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return len(values) + 1
}
