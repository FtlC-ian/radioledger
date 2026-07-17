package adif

import (
	"strings"
	"unicode"
)

// KnownSuffixes are ADIF-recognized portable/mobile suffix modifiers.
// These appear after the final '/' in a callsign.
var KnownSuffixes = map[string]SuffixInfo{
	"P":   {Code: "P", Description: "Portable"},
	"M":   {Code: "M", Description: "Mobile"},
	"MM":  {Code: "MM", Description: "Maritime Mobile"},
	"AM":  {Code: "AM", Description: "Aeronautical Mobile"},
	"QRP": {Code: "QRP", Description: "Low power (5W or less)"},
	"A":   {Code: "A", Description: "Alternative (some logging programs)"},
	"R":   {Code: "R", Description: "Rover (ARRL VHF contests)"},
}

// SuffixInfo describes a portable/mobile operation suffix.
type SuffixInfo struct {
	Code        string
	Description string
}

// ParsedCallsign holds the decomposed parts of a callsign.
// For example, "VK9/W5XXX/P" yields:
//
//	Raw:            "VK9/W5XXX/P"
//	Base:           "W5XXX"
//	PrefixOverride: "VK9"
//	Suffix:         "P"
//	WPXPrefix:      "VK9"  (prefix override takes precedence)
type ParsedCallsign struct {
	// Raw is the callsign exactly as input, uppercased.
	Raw string

	// Base is the core callsign without any portable prefix or suffix.
	// e.g., "W5XXX" from "VK9/W5XXX/P"
	Base string

	// PrefixOverride is set when a non-call-area prefix precedes the call.
	// e.g., "VK9" from "VK9/W5XXX" — this is the DXCC entity prefix, not USA.
	// Empty if no prefix override is present.
	PrefixOverride string

	// Suffix is the portable/mobile modifier, if any.
	// e.g., "P" from "W5XXX/P", "MM" from "W5XXX/MM"
	Suffix string

	// SuffixInfo describes the suffix if it's a known modifier. Nil if unknown/absent.
	SuffixInfo *SuffixInfo

	// WPXPrefix is the callsign prefix for WPX award tracking.
	// For prefix overrides, this is the prefix override.
	// For standard calls, this is derived from the base callsign.
	WPXPrefix string
}

// ParseCallsign parses a callsign into its component parts.
// Input is trimmed and uppercased. DXCC entity lookup is NOT performed here;
// that requires a database query (DXCC resolution uses the dxcc_entities table).
//
// Parsing rules:
//
//  1. Uppercase and trim whitespace.
//  2. Split on '/' characters.
//  3. If 3+ parts: middle part is the base, first is prefix override, last is suffix.
//  4. If 2 parts: the second part is a known suffix modifier → first part is base.
//     Otherwise, if the second part looks like a callsign (has digit) → first is prefix override.
//     Otherwise (short, no digit) → treat second part as an unknown suffix.
//  5. If 1 part: it's the base call.
func ParseCallsign(raw string) *ParsedCallsign {
	upper := strings.ToUpper(strings.TrimSpace(raw))
	if upper == "" {
		return &ParsedCallsign{Raw: upper, Base: upper}
	}

	parts := strings.Split(upper, "/")

	pc := &ParsedCallsign{Raw: upper}

	switch len(parts) {
	case 1:
		pc.Base = parts[0]

	case 2:
		left, right := parts[0], parts[1]
		if info, ok := KnownSuffixes[right]; ok {
			// Known suffix modifier: W5XXX/P, W5XXX/MM, etc.
			pc.Base = left
			pc.Suffix = right
			pc.SuffixInfo = &info
		} else if looksLikeCall(right) {
			// Right part resembles a callsign: VK9/W5XXX, 5B4/G3ZZZ
			// Left is the DXCC prefix override.
			pc.PrefixOverride = left
			pc.Base = right
		} else {
			// Unknown suffix modifier
			pc.Base = left
			pc.Suffix = right
		}

	default:
		// 3+ parts: PREFIX/BASE/SUFFIX
		pc.PrefixOverride = parts[0]
		pc.Base = parts[1]
		pc.Suffix = strings.Join(parts[2:], "/")
		if info, ok := KnownSuffixes[pc.Suffix]; ok {
			pc.SuffixInfo = &info
		}
	}

	// Derive WPX prefix
	if pc.PrefixOverride != "" {
		pc.WPXPrefix = pc.PrefixOverride
	} else {
		pc.WPXPrefix = extractWPXPrefix(pc.Base)
	}

	return pc
}

// looksLikeCall returns true if s resembles a callsign segment (has both letters and a digit).
// Used to distinguish "VK9/W5XXX" (prefix override) from "W5XXX/P" (portable suffix).
func looksLikeCall(s string) bool {
	if len(s) < 3 {
		return false
	}
	hasDigit := false
	hasLetter := false
	for _, c := range s {
		if unicode.IsDigit(c) {
			hasDigit = true
		}
		if unicode.IsLetter(c) {
			hasLetter = true
		}
	}
	return hasDigit && hasLetter
}

// extractWPXPrefix derives the WPX award prefix from a base callsign.
//
// WPX prefix rule: include all characters from the start up to and including
// the first digit that appears AFTER the first letter. This handles both
// standard calls (W1AW → W1, VE3XYZ → VE3) and calls starting with digits
// (3W3RR → 3W3, 9A1AA → 9A1).
//
// Examples:
//
//	W1AW   → W1    (letter then first digit)
//	VE3XYZ → VE3   (two letters then first digit)
//	DL1ABC → DL1   (two letters then first digit)
//	JA1ABC → JA1
//	3W3RR  → 3W3   (starts with digit, then letters, then next digit)
//	9A1AA  → 9A1   (starts with digit, then letter, then digit)
func extractWPXPrefix(call string) string {
	if call == "" {
		return ""
	}

	runes := []rune(call)

	// Find the index of the first letter in the callsign.
	firstLetterIdx := -1
	for i, c := range runes {
		if unicode.IsLetter(c) {
			firstLetterIdx = i
			break
		}
	}
	if firstLetterIdx == -1 {
		// No letter at all (pure numeric): return as-is (unusual)
		return call
	}

	// Find the first digit AFTER the first letter.
	for i := firstLetterIdx; i < len(runes); i++ {
		if unicode.IsDigit(runes[i]) {
			// Prefix includes everything up to and including this digit.
			return string(runes[:i+1])
		}
	}

	// No digit after first letter (unusual): return full call as prefix.
	return call
}

// NormalizeCallsign returns the callsign in uppercase, trimmed.
// This is the minimal normalization applied on import before DXCC resolution.
func NormalizeCallsign(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
