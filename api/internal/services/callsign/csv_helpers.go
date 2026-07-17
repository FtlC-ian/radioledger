package callsign

import "strings"

// csvColumnIndex returns a map of lowercase column name → index.
// Used by CSV-based sync workers (POTA, SOTA).
func csvColumnIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return m
}

// safeCol returns row[i] if i is a valid index, else "".
func safeCol(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}
