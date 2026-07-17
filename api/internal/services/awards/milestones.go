package awards

// AwardType is the canonical string identifier for an award program.
// Matches the CHECK constraint values in award_progress.award_type.
type AwardType string

const (
	AwardDXCC          AwardType = "dxcc"
	AwardWAS           AwardType = "was"
	AwardVUCC          AwardType = "vucc"
	AwardWAZ           AwardType = "waz"
	AwardWPX           AwardType = "wpx"
	AwardPOTAHunter    AwardType = "pota_hunter"
	AwardPOTAActivator AwardType = "pota_activator"
	AwardSOTAChaser    AwardType = "sota_chaser"
	AwardSOTAActivator AwardType = "sota_activator"
)

// ValidAwardTypes returns all supported award type identifiers.
func ValidAwardTypes() []AwardType {
	return []AwardType{
		AwardDXCC, AwardWAS, AwardVUCC, AwardWAZ, AwardWPX,
		AwardPOTAHunter, AwardPOTAActivator,
		AwardSOTAChaser, AwardSOTAActivator,
	}
}

// IsValidAwardType returns true if the string is a recognised award type.
func IsValidAwardType(s string) bool {
	for _, at := range ValidAwardTypes() {
		if string(at) == s {
			return true
		}
	}
	return false
}

// Milestone is a progress threshold that, when crossed, triggers a
// user-facing notification (e.g. "You've worked 100 DXCC entities!").
type Milestone struct {
	Count   int64
	Label   string // Human-readable description for the notification payload
}

// MilestonesFor returns the ordered milestone thresholds for an award type.
// The caller should iterate and fire notifications when Count is reached.
func MilestonesFor(at AwardType) []Milestone {
	switch at {
	case AwardDXCC:
		return []Milestone{
			{100, "DXCC — 100 entities worked"},
			{200, "DXCC — 200 entities worked"},
			{300, "DXCC — Honor Roll (300+)"},
			{330, "DXCC — Near-complete (330+)"},
		}
	case AwardWAS:
		return []Milestone{
			{10, "WAS — 10 states"},
			{25, "WAS — 25 states"},
			{50, "WAS — All 50 states!"},
		}
	case AwardVUCC:
		return []Milestone{
			{100, "VUCC — Basic (100 grids)"},
			{500, "VUCC — 500 grids"},
			{1000, "VUCC — 1000 grids"},
		}
	case AwardWAZ:
		return []Milestone{
			{10, "WAZ — 10 zones"},
			{20, "WAZ — 20 zones"},
			{40, "WAZ — All 40 zones!"},
		}
	case AwardWPX:
		return []Milestone{
			{100, "WPX — 100 prefixes"},
			{300, "WPX — 300 prefixes"},
			{600, "WPX — 600 prefixes"},
			{1000, "WPX — 1000 prefixes"},
		}
	case AwardPOTAHunter:
		return []Milestone{
			{10, "POTA Hunter — 10 parks"},
			{100, "POTA Hunter — 100 parks"},
			{500, "POTA Hunter — 500 parks"},
			{1000, "POTA Hunter — 1000 parks"},
		}
	case AwardPOTAActivator:
		return []Milestone{
			{1, "POTA Activator — First activation!"},
			{10, "POTA Activator — 10 activations"},
			{50, "POTA Activator — 50 activations"},
			{100, "POTA Activator — 100 activations"},
		}
	case AwardSOTAChaser:
		return []Milestone{
			{1, "SOTA Chaser — First summit chased!"},
			{100, "SOTA Chaser — 100 summits"},
			{500, "SOTA Chaser — Shack Sloth (500+)"},
			{1000, "SOTA Chaser — 1000 summits"},
		}
	case AwardSOTAActivator:
		return []Milestone{
			{1, "SOTA Activator — First summit activated!"},
			{100, "SOTA Activator — Mountain Goat (100+)"},
			{250, "SOTA Activator — 250 summits"},
			{500, "SOTA Activator — 500 summits"},
		}
	}
	return nil
}

// TotalTarget returns the canonical "goal" count for an award type.
// Used to compute percentage progress and "needed" counts.
func TotalTarget(at AwardType) int64 {
	switch at {
	case AwardDXCC:
		return 340 // current DXCC entity count (updated periodically by ARRL)
	case AwardWAS:
		return 50
	case AwardVUCC:
		return 100 // VUCC basic
	case AwardWAZ:
		return 40
	case AwardWPX:
		return 0 // unbounded — no canonical total
	case AwardPOTAHunter, AwardPOTAActivator:
		return 0 // unbounded
	case AwardSOTAChaser, AwardSOTAActivator:
		return 0 // unbounded
	}
	return 0
}
