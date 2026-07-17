// Package awards provides award calculation helpers for RadioLedger.
// Each function extracts the key identifier for a specific award type
// from a QSO field, enabling consistent prefix/zone/state computation
// across both live queries and the background refresh worker.
package awards

import (
	"strings"
	"unicode"
)

// WPXPrefix extracts the WPX (Worked All Prefixes) prefix from a callsign
// using the standard ITU/CQ WPX rules:
//
//  1. Strip any portable suffix (e.g. "/P", "/M", "/QRP") — unless the
//     suffix indicates a different DXCC entity (e.g. "W6/DL1ABC" → "W6").
//  2. The prefix is the letters before the first digit PLUS the first digit.
//     Example: "W1AW"   → "W1"
//     Example: "DL1ABC" → "DL1"
//     Example: "VK9/W1AW" → use left side of "/" (portable from foreign entity)
//
// Returns an empty string if the callsign contains no digit (malformed).
func WPXPrefix(callsign string) string {
	if callsign == "" {
		return ""
	}

	cs := strings.ToUpper(strings.TrimSpace(callsign))

	// Handle portable designators: if there's a "/" pick the prefix-bearing part.
	// Rule: use the part that contains the operator's entity prefix.
	// Heuristic: if left part is 1-2 chars (prefix-only like "W6", "VK9"), use it.
	//            otherwise use the left part as the base callsign.
	if idx := strings.Index(cs, "/"); idx != -1 {
		left := cs[:idx]
		right := cs[idx+1:]
		// Ignore suffix portables like /P, /M, /QRP, /MM
		if isPortableSuffix(right) {
			cs = left
		} else if isPortableSuffix(left) {
			// e.g. "P/DL1ABC" — unusual, use right
			cs = right
		} else {
			// e.g. "W6/DL1ABC" or "VK9X/W1AW" — left part is the entity prefix
			cs = left
		}
	}

	// Find the first digit position.
	firstDigit := -1
	for i, ch := range cs {
		if unicode.IsDigit(ch) {
			firstDigit = i
			break
		}
	}

	if firstDigit == -1 {
		// No digit found — some special callsigns (e.g. SWL, club calls without number).
		// Return the full callsign as a prefix fallback.
		return cs
	}

	// Prefix = everything up to and including the first digit.
	return cs[:firstDigit+1]
}

// isPortableSuffix returns true for common portable suffixes that do NOT
// indicate a different DXCC entity.
func isPortableSuffix(s string) bool {
	switch s {
	case "P", "M", "AM", "MM", "QRP", "QRPP", "LGT", "A", "B", "C":
		return true
	}
	// Single digit suffix is a district indicator (W1AW/6 → W6), treat as entity prefix
	if len(s) == 1 && unicode.IsDigit(rune(s[0])) {
		return false
	}
	return false
}
