package migrations

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExpectedGooseVersionMatchesHighestMigrationFile(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "..", "database", "migrations"))
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}

	var highest int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		prefix := strings.SplitN(entry.Name(), "_", 2)[0]
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("parse migration version from %q: %v", entry.Name(), err)
		}
		if version > highest {
			highest = version
		}
	}

	if highest == 0 {
		t.Fatal("no SQL migrations found")
	}
	if ExpectedGooseVersion != highest {
		t.Fatalf("ExpectedGooseVersion=%d, highest migration file=%d", ExpectedGooseVersion, highest)
	}
}
