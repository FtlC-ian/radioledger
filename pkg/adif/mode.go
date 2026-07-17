package adif

import "strings"

// ModeValue represents the canonical ADIF MODE/SUBMODE pair to export.
type ModeValue struct {
	Mode    string
	Submode string
}

// CanonicalADIFModes is the set of ADIF 3.1.7 MODE values valid for export.
var CanonicalADIFModes = map[string]struct{}{
	"AM": {}, "ARDOP": {}, "ATV": {}, "CHIP": {}, "CLO": {}, "CONTESTI": {},
	"CW": {}, "DIGITALVOICE": {}, "DOMINO": {}, "DYNAMIC": {}, "FAX": {}, "FM": {},
	"FSK": {}, "FSK441": {}, "FT8": {}, "HELL": {}, "ISCAT": {}, "JT4": {},
	"JT6M": {}, "JT9": {}, "JT44": {}, "JT65": {}, "MFSK": {}, "MSK144": {},
	"MT63": {}, "MTONE": {}, "OFDM": {}, "OLIVIA": {}, "OPERA": {}, "PAC": {},
	"PAX": {}, "PKT": {}, "PSK": {}, "PSK2K": {}, "Q15": {}, "QRA64": {},
	"ROS": {}, "RTTY": {}, "RTTYM": {}, "SSB": {}, "SSTV": {}, "T10": {},
	"THOR": {}, "THRB": {}, "TOR": {}, "V4": {}, "VOI": {}, "WINMOR": {},
	"WSPR": {},
}

// CanonicalADIFSubmodes maps ADIF 3.1.7 SUBMODE values to their canonical MODE.
// This is intentionally focused on values RadioLedger currently tolerates or emits.
var CanonicalADIFSubmodes = map[string]string{
	"USB":            "SSB",
	"LSB":            "SSB",
	"PCW":            "CW",
	"ASCI":           "RTTY",
	"C4FM":           "DIGITALVOICE",
	"DMR":            "DIGITALVOICE",
	"DSTAR":          "DIGITALVOICE",
	"FREEDV":         "DIGITALVOICE",
	"M17":            "DIGITALVOICE",
	"FREEDATA":       "DYNAMIC",
	"VARA HF":        "DYNAMIC",
	"VARA SATELLITE": "DYNAMIC",
	"VARA FM 1200":   "DYNAMIC",
	"VARA FM 9600":   "DYNAMIC",
	"SCAMP_FAST":     "FSK",
	"SCAMP_SLOW":     "FSK",
	"SCAMP_VSLOW":    "FSK",
	"SCAMP_OO":       "MTONE",
	"SCAMP_OO_SLW":   "MTONE",
	"RIBBIT_PIX":     "OFDM",
	"RIBBIT_SMS":     "OFDM",
	"FSQCALL":        "MFSK",
	"FST4":           "MFSK",
	"FST4W":          "MFSK",
	"FT2":            "MFSK",
	"FT4":            "MFSK",
	"JS8":            "MFSK",
	"JTMS":           "MFSK",
	"MFSK4":          "MFSK",
	"MFSK8":          "MFSK",
	"MFSK11":         "MFSK",
	"MFSK16":         "MFSK",
	"MFSK22":         "MFSK",
	"MFSK31":         "MFSK",
	"MFSK32":         "MFSK",
	"MFSK64":         "MFSK",
	"MFSK64L":        "MFSK",
	"MFSK128":        "MFSK",
	"MFSK128L":       "MFSK",
	"Q65":            "MFSK",
	"PSK31":          "PSK",
	"PSK63":          "PSK",
}

// ModeAliases maps tolerated import or UI aliases to their canonical export pair.
// Aliases include ADIF import-only MODE values plus a small set of RadioLedger UI names.
// Bare submode-as-MODE import aliases are populated from CanonicalADIFSubmodes below
// so import accepts common third-party exports while export stays canonical.
var ModeAliases = buildModeAliases()

func buildModeAliases() map[string]ModeValue {
	aliases := map[string]ModeValue{
		// ADIF import-only MODE aliases.
		"AMTORFEC": {Mode: "TOR", Submode: "AMTORFEC"},
		"ASCI":     {Mode: "RTTY", Submode: "ASCI"},
		"C4FM":     {Mode: "DIGITALVOICE", Submode: "C4FM"},
		"CHIP64":   {Mode: "CHIP", Submode: "CHIP64"},
		"CHIP128":  {Mode: "CHIP", Submode: "CHIP128"},
		"DOMINOF":  {Mode: "DOMINO", Submode: "DOMINOF"},
		"DSTAR":    {Mode: "DIGITALVOICE", Submode: "DSTAR"},
		"FMHELL":   {Mode: "HELL", Submode: "FMHELL"},
		"FSK31":    {Mode: "PSK", Submode: "FSK31"},
		"GTOR":     {Mode: "TOR", Submode: "GTOR"},
		"HELL80":   {Mode: "HELL", Submode: "HELL80"},
		"HFSK":     {Mode: "HELL", Submode: "HFSK"},
		"JT4A":     {Mode: "JT4", Submode: "JT4A"},
		"JT4B":     {Mode: "JT4", Submode: "JT4B"},
		"JT4C":     {Mode: "JT4", Submode: "JT4C"},
		"JT4D":     {Mode: "JT4", Submode: "JT4D"},
		"JT4E":     {Mode: "JT4", Submode: "JT4E"},
		"JT4F":     {Mode: "JT4", Submode: "JT4F"},
		"JT4G":     {Mode: "JT4", Submode: "JT4G"},
		"JT65A":    {Mode: "JT65", Submode: "JT65A"},
		"JT65B":    {Mode: "JT65", Submode: "JT65B"},
		"JT65C":    {Mode: "JT65", Submode: "JT65C"},
		"MFSK8":    {Mode: "MFSK", Submode: "MFSK8"},
		"MFSK16":   {Mode: "MFSK", Submode: "MFSK16"},
		"PAC2":     {Mode: "PAC", Submode: "PAC2"},
		"PAC3":     {Mode: "PAC", Submode: "PAC3"},
		"PAX2":     {Mode: "PAX", Submode: "PAX2"},
		"PCW":      {Mode: "CW", Submode: "PCW"},
		"PSK10":    {Mode: "PSK", Submode: "PSK10"},
		"PSK31":    {Mode: "PSK", Submode: "PSK31"},
		"PSK63":    {Mode: "PSK", Submode: "PSK63"},
		"PSK63F":   {Mode: "PSK", Submode: "PSK63F"},
		"PSK125":   {Mode: "PSK", Submode: "PSK125"},
		"PSKAM10":  {Mode: "PSK", Submode: "PSKAM10"},
		"PSKAM31":  {Mode: "PSK", Submode: "PSKAM31"},
		"PSKAM50":  {Mode: "PSK", Submode: "PSKAM50"},
		"PSKFEC31": {Mode: "PSK", Submode: "PSKFEC31"},
		"PSKHELL":  {Mode: "HELL", Submode: "PSKHELL"},
		"QPSK31":   {Mode: "PSK", Submode: "QPSK31"},
		"QPSK63":   {Mode: "PSK", Submode: "QPSK63"},
		"QPSK125":  {Mode: "PSK", Submode: "QPSK125"},
		"THRBX":    {Mode: "THRB", Submode: "THRBX"},

		// RadioLedger-friendly aliases that should export canonically.
		"DMR":           {Mode: "DIGITALVOICE", Submode: "DMR"},
		"FREEDATA":      {Mode: "DYNAMIC", Submode: "FREEDATA"},
		"FREEDV":        {Mode: "DIGITALVOICE", Submode: "FREEDV"},
		"FT2":           {Mode: "MFSK", Submode: "FT2"},
		"FT4":           {Mode: "MFSK", Submode: "FT4"},
		"JS8":           {Mode: "MFSK", Submode: "JS8"},
		"LSB":           {Mode: "SSB", Submode: "LSB"},
		"M17":           {Mode: "DIGITALVOICE", Submode: "M17"},
		"PACKET":        {Mode: "PKT"},
		"Q65":           {Mode: "MFSK", Submode: "Q65"},
		"USB":           {Mode: "SSB", Submode: "USB"},
		"VARA":          {Mode: "DYNAMIC", Submode: "VARA HF"},
		"VARAFH":        {Mode: "DYNAMIC", Submode: "VARA HF"},
		"VARAHF":        {Mode: "DYNAMIC", Submode: "VARA HF"},
		"VARAFM1200":    {Mode: "DYNAMIC", Submode: "VARA FM 1200"},
		"VARAFM9600":    {Mode: "DYNAMIC", Submode: "VARA FM 9600"},
		"VARASAT":       {Mode: "DYNAMIC", Submode: "VARA SATELLITE"},
		"VARASATELLITE": {Mode: "DYNAMIC", Submode: "VARA SATELLITE"},
	}

	// Auto-derive bare-MODE import aliases for every ADIF 3.1.7 submode not
	// already in the explicit alias map above.  This keeps the alias set
	// in sync with CanonicalADIFSubmodes automatically: adding a new submode
	// there is sufficient for it to become a valid bare-MODE import alias.
	for submode, mode := range CanonicalADIFSubmodes {
		if _, exists := aliases[submode]; exists {
			continue
		}
		aliases[submode] = ModeValue{Mode: mode, Submode: submode}
	}

	return aliases
}

// KnownModes contains all mode strings RadioLedger accepts as MODE input.
var KnownModes = buildKnownModes()

func buildKnownModes() map[string]bool {
	out := make(map[string]bool, len(CanonicalADIFModes)+len(ModeAliases))
	for mode := range CanonicalADIFModes {
		out[mode] = true
	}
	for alias := range ModeAliases {
		out[alias] = true
	}
	return out
}

// ResolveMode maps a raw MODE string to its canonical ADIF export pair.
func ResolveMode(mode string) (ModeValue, bool) {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	if mode == "" {
		return ModeValue{}, false
	}
	if canonical, ok := ModeAliases[mode]; ok {
		return canonical, true
	}
	if _, ok := CanonicalADIFModes[mode]; ok {
		return ModeValue{Mode: mode}, true
	}
	return ModeValue{}, false
}

// NormalizeModePair canonicalizes MODE/SUBMODE for ADIF export.
// It accepts legacy direct-mode aliases such as USB, DSTAR, PSK31, or VARAHF.
func NormalizeModePair(mode, submode string) (ModeValue, bool) {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	submode = strings.ToUpper(strings.TrimSpace(submode))
	if mode == "" {
		return ModeValue{}, false
	}

	if submode == "" {
		return ResolveMode(mode)
	}

	resolvedMode, ok := ResolveMode(mode)
	if !ok {
		return ModeValue{}, false
	}
	if resolvedMode.Submode != "" && resolvedMode.Submode != submode {
		return ModeValue{}, false
	}
	if resolvedMode.Submode == submode {
		return resolvedMode, true
	}
	canonicalMode := resolvedMode.Mode
	if expectedMode, ok := CanonicalADIFSubmodes[submode]; ok && expectedMode == canonicalMode {
		return ModeValue{Mode: canonicalMode, Submode: submode}, true
	}
	if resolvedSubmode, ok := ResolveMode(submode); ok {
		if resolvedSubmode.Mode == canonicalMode && resolvedSubmode.Submode == submode {
			return ModeValue{Mode: canonicalMode, Submode: submode}, true
		}
		if canonicalMode == "MFSK" && resolvedSubmode.Mode == submode && resolvedSubmode.Submode == "" {
			return resolvedSubmode, true
		}
	}
	return ModeValue{}, false
}

// CanonicalizeRecordMode rewrites MODE/SUBMODE in-place to canonical ADIF export
// values when the stored pair is known. It leaves unknown values untouched.
// Returns true when a known canonical pair was applied.
func CanonicalizeRecordMode(rec *Record) bool {
	if rec == nil {
		return false
	}
	mode := rec.Get("MODE")
	submode := rec.Get("SUBMODE")
	canonical, ok := NormalizeModePair(mode, submode)
	if !ok {
		return false
	}
	rec.Set("MODE", canonical.Mode)
	if canonical.Submode == "" {
		rec.Delete("SUBMODE")
	} else {
		rec.Set("SUBMODE", canonical.Submode)
	}
	return true
}
