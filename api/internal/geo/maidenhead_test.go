package geo

import (
	"math"
	"strings"
	"testing"
)

func TestLatLonToGrid_W1AW(t *testing.T) {
	grid := LatLonToGrid(41.7148, -72.7272)
	if grid != "FN31pr" {
		t.Fatalf("LatLonToGrid(W1AW): got %q want %q", grid, "FN31pr")
	}
}

func TestLatLonToGrid_AF5SHMaumelle(t *testing.T) {
	grid := LatLonToGrid(34.8668, -92.4040)
	if !strings.HasPrefix(grid, "EM34") && !strings.HasPrefix(grid, "EM35") {
		t.Fatalf("LatLonToGrid(AF5SH/Maumelle): got %q want prefix EM34 or EM35", grid)
	}
}

func TestLatLonToGrid_NullIsland(t *testing.T) {
	grid := LatLonToGrid(0, 0)
	if grid != "JJ00aa" {
		t.Fatalf("LatLonToGrid(0,0): got %q want %q", grid, "JJ00aa")
	}
}

func TestGridToLatLon_W1AW(t *testing.T) {
	lat, lon := GridToLatLon("FN31pr")
	if math.Abs(lat-41.7291667) > 0.02 {
		t.Fatalf("GridToLatLon lat: got %.6f want about %.6f", lat, 41.7291667)
	}
	if math.Abs(lon-(-72.7083333)) > 0.02 {
		t.Fatalf("GridToLatLon lon: got %.6f want about %.6f", lon, -72.7083333)
	}
}

func TestGridToLatLon_NullIsland(t *testing.T) {
	lat, lon := GridToLatLon("jj00aa")
	if math.Abs(lat-0.0208333) > 0.0001 {
		t.Fatalf("GridToLatLon lat: got %.6f want about %.6f", lat, 0.0208333)
	}
	if math.Abs(lon-0.0416667) > 0.0001 {
		t.Fatalf("GridToLatLon lon: got %.6f want about %.6f", lon, 0.0416667)
	}
}
