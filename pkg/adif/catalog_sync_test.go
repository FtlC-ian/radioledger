package adif_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestSQLBandCatalogsStayInSyncWithGo(t *testing.T) {
	seed := readBandCatalog(t, repoPath(t, "database", "seeds", "001_reference_data.sql"))
	migration := readBandCatalog(t, repoPath(t, "database", "migrations", "001_initial_schema.sql"))
	want := map[string]adif.BandDef{}
	for name, def := range adif.KnownBands {
		want[name] = def
	}

	assertBandCatalogEqual(t, "seed vs Go", seed, want)
	assertBandCatalogEqual(t, "migration vs Go", migration, want)
	assertBandCatalogEqual(t, "seed vs migration", seed, migration)
}

func TestSQLModeCatalogsIncludeCanonicalADIFModes(t *testing.T) {
	seed := readModeCatalog(t, repoPath(t, "database", "seeds", "001_reference_data.sql"))
	migration := readModeCatalog(t, repoPath(t, "database", "migrations", "001_initial_schema.sql"))

	if diff := missingCanonicalModes(seed); len(diff) > 0 {
		t.Fatalf("seed modes missing canonical ADIF modes: %v", diff)
	}
	if diff := missingCanonicalModes(migration); len(diff) > 0 {
		t.Fatalf("migration modes missing canonical ADIF modes: %v", diff)
	}
	if diff := symmetricDiff(seed, migration); len(diff) > 0 {
		t.Fatalf("seed and migration mode catalogs differ: %v", diff)
	}
}

func repoPath(t *testing.T, parts ...string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	base := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return filepath.Join(append([]string{base}, parts...)...)
}

func readBandCatalog(t *testing.T, path string) map[string]adif.BandDef {
	t.Helper()
	block := readInsertBlock(t, path, "bands")
	pattern := regexp.MustCompile(`\('([^']+)',\s*([0-9.]+),\s*([0-9.]+),`)
	out := make(map[string]adif.BandDef)
	for _, match := range pattern.FindAllStringSubmatch(block, -1) {
		lower, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			t.Fatalf("parse lower freq for %s in %s: %v", match[1], path, err)
		}
		upper, err := strconv.ParseFloat(match[3], 64)
		if err != nil {
			t.Fatalf("parse upper freq for %s in %s: %v", match[1], path, err)
		}
		out[match[1]] = adif.BandDef{Name: match[1], LowerMHz: lower, UpperMHz: upper}
	}
	return out
}

func readModeCatalog(t *testing.T, path string) map[string]struct{} {
	t.Helper()
	block := readInsertBlock(t, path, "modes")
	pattern := regexp.MustCompile(`\('([^']+)',`)
	out := make(map[string]struct{})
	for _, match := range pattern.FindAllStringSubmatch(block, -1) {
		out[match[1]] = struct{}{}
	}
	return out
}

func readInsertBlock(t *testing.T, path, table string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`(?s)INSERT INTO %s .*? VALUES\n(.*?)\nON CONFLICT`, regexp.QuoteMeta(table)))
	match := pattern.FindSubmatch(content)
	if match == nil {
		t.Fatalf("could not find INSERT block for %s in %s", table, path)
	}
	return string(match[1])
}

func assertBandCatalogEqual(t *testing.T, label string, got, want map[string]adif.BandDef) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s count mismatch: got %d, want %d", label, len(got), len(want))
	}
	for name, wantDef := range want {
		gotDef, ok := got[name]
		if !ok {
			t.Fatalf("%s missing %s", label, name)
		}
		if gotDef != wantDef {
			t.Fatalf("%s mismatch for %s: got %+v, want %+v", label, name, gotDef, wantDef)
		}
	}
}

func missingCanonicalModes(have map[string]struct{}) []string {
	missing := []string{}
	for mode := range adif.CanonicalADIFModes {
		if _, ok := have[mode]; !ok {
			missing = append(missing, mode)
		}
	}
	sort.Strings(missing)
	return missing
}

func symmetricDiff(a, b map[string]struct{}) []string {
	diff := []string{}
	seen := map[string]struct{}{}
	for name := range a {
		if _, ok := b[name]; !ok {
			diff = append(diff, name)
		}
		seen[name] = struct{}{}
	}
	for name := range b {
		if _, ok := seen[name]; ok {
			continue
		}
		if _, ok := a[name]; !ok {
			diff = append(diff, name)
		}
	}
	sort.Strings(diff)
	return diff
}

func TestSQLBandRegionAllocationsUseCanonicalBandNames(t *testing.T) {
	path := repoPath(t, "database", "migrations", "001_initial_schema.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(content)
	if strings.Contains(text, "'2200m'") {
		t.Fatalf("migration still references legacy 2200m band name")
	}
	if !strings.Contains(text, "'2190m'") {
		t.Fatalf("migration missing canonical 2190m band name")
	}
}
