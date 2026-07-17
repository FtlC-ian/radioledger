package adif

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ValidationError represents a field-level validation failure.
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("adif validation: field %s value %q: %s", e.Field, e.Value, e.Message)
}

// ValidationResult collects all validation issues for a record.
type ValidationResult struct {
	Errors   []*ValidationError
	Warnings []*ValidationError
}

// OK returns true if there are no errors (warnings are allowed).
func (r *ValidationResult) OK() bool {
	return len(r.Errors) == 0
}

// ValidateRecord validates all known fields in a record.
// Unknown fields (APP_* etc.) are passed through without validation.
func ValidateRecord(rec *Record) *ValidationResult {
	result := &ValidationResult{}

	for _, f := range rec.Fields {
		if errs := ValidateField(f.Name, f.Value); len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
		}
	}
	return result
}

// ValidateField validates a single ADIF field value by name.
// Returns a slice of ValidationErrors (empty if valid).
func ValidateField(name, value string) []*ValidationError {
	name = strings.ToUpper(name)

	switch name {
	case "QSO_DATE", "QSO_DATE_OFF":
		if err := ValidateDate(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "TIME_ON", "TIME_OFF":
		if err := ValidateTime(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "FREQ", "FREQ_RX":
		if err := ValidateFrequency(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "BAND", "BAND_RX":
		if err := ValidateBand(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "MODE":
		if err := ValidateMode(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "CALL":
		if err := ValidateCallsign(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "GRIDSQUARE", "MY_GRIDSQUARE":
		if err := ValidateGridsquare(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	case "CQZ":
		if n, err := strconv.Atoi(value); err != nil || n < 1 || n > 40 {
			return []*ValidationError{{Field: name, Value: value, Message: "must be 1-40"}}
		}
	case "ITUZ":
		if n, err := strconv.Atoi(value); err != nil || n < 1 || n > 90 {
			return []*ValidationError{{Field: name, Value: value, Message: "must be 1-90"}}
		}
	case "CONT":
		valid := map[string]bool{"NA": true, "SA": true, "EU": true, "AF": true, "AS": true, "OC": true, "AN": true}
		if !valid[strings.ToUpper(value)] {
			return []*ValidationError{{Field: name, Value: value, Message: "must be NA,SA,EU,AF,AS,OC,AN"}}
		}
	case "QSLSDATE", "QSLRDATE", "LOTW_QSLSDATE", "LOTW_QSLRDATE", "EQSL_QSLSDATE", "EQSL_QSLRDATE":
		if err := ValidateDate(value); err != nil {
			return []*ValidationError{{Field: name, Value: value, Message: err.Error()}}
		}
	}
	return nil
}

// ValidateDate validates an ADIF date string (YYYYMMDD format).
// The year must be in a reasonable range (1900-2100).
func ValidateDate(s string) error {
	s = strings.TrimSpace(s)
	if len(s) != 8 {
		return fmt.Errorf("date must be 8 digits (YYYYMMDD), got %d chars", len(s))
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return fmt.Errorf("date must contain only digits, got %q", s)
		}
	}

	year, _ := strconv.Atoi(s[0:4])
	month, _ := strconv.Atoi(s[4:6])
	day, _ := strconv.Atoi(s[6:8])

	if year < 1900 || year > 2200 {
		return fmt.Errorf("year %d is outside reasonable range (1900-2200)", year)
	}
	if month < 1 || month > 12 {
		return fmt.Errorf("month %d is invalid (1-12)", month)
	}

	// Validate day using time.Date (handles leap years, month lengths)
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return fmt.Errorf("invalid date %s (day %d is out of range for month %d)", s, day, month)
	}
	return nil
}

// ValidateTime validates an ADIF time string (HHMM or HHMMSS format).
func ValidateTime(s string) error {
	s = strings.TrimSpace(s)
	if len(s) != 4 && len(s) != 6 {
		return fmt.Errorf("time must be 4 (HHMM) or 6 (HHMMSS) digits, got %d chars", len(s))
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return fmt.Errorf("time must contain only digits, got %q", s)
		}
	}

	hour, _ := strconv.Atoi(s[0:2])
	min, _ := strconv.Atoi(s[2:4])
	if hour > 23 {
		return fmt.Errorf("hour %d is invalid (0-23)", hour)
	}
	if min > 59 {
		return fmt.Errorf("minute %d is invalid (0-59)", min)
	}

	if len(s) == 6 {
		sec, _ := strconv.Atoi(s[4:6])
		if sec > 59 {
			return fmt.Errorf("second %d is invalid (0-59)", sec)
		}
	}
	return nil
}

// ValidateFrequency validates an ADIF frequency string in MHz.
// Range: 0.001 to 300000 MHz.
func ValidateFrequency(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("frequency is empty")
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("frequency %q is not a valid number: %w", s, err)
	}
	if f <= 0 {
		return fmt.Errorf("frequency must be positive, got %g", f)
	}
	if f < 0.001 || f > 300000 {
		return fmt.Errorf("frequency %g MHz is outside range 0.001-300000 MHz", f)
	}
	return nil
}

// ValidateBand validates an ADIF band string against the known band list.
func ValidateBand(s string) error {
	s = strings.ToLower(strings.TrimSpace(s))
	if _, ok := KnownBands[s]; ok {
		return nil
	}
	return fmt.Errorf("band %q is not a recognized amateur radio band", s)
}

// ValidateMode validates an ADIF MODE string.
// It accepts canonical ADIF 3.1.7 export values and a bounded set of tolerated
// import aliases that NormalizeModePair can rewrite into canonical MODE/SUBMODE.
func ValidateMode(s string) error {
	if _, ok := ResolveMode(s); ok {
		return nil
	}
	return fmt.Errorf("mode %q is not a recognized ADIF mode", s)
}

// ValidateCallsign performs loose callsign format validation.
// Ham radio callsigns vary widely internationally, so we only reject obviously wrong values.
// Valid callsigns: start with a letter or digit, contain only letters, digits, and /
// The / character is used for portable/prefix operations: VK9/W5XXX, W5XXX/P
func ValidateCallsign(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("callsign is empty")
	}
	if len(s) > 20 {
		return fmt.Errorf("callsign %q is too long (max 20 chars)", s)
	}

	for i, c := range strings.ToUpper(s) {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '/' {
			return fmt.Errorf("callsign %q contains invalid character %q at position %d", s, c, i)
		}
	}

	// Must contain at least one letter and one digit
	hasLetter := false
	hasDigit := false
	for _, c := range s {
		if unicode.IsLetter(c) {
			hasLetter = true
		}
		if unicode.IsDigit(c) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("callsign %q must contain at least one letter and one digit", s)
	}

	return nil
}

// ValidateGridsquare validates a Maidenhead grid locator (4 or 6 characters).
func ValidateGridsquare(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("grid square is empty")
	}
	upper := strings.ToUpper(s)
	if len(upper) < 4 || (len(upper) != 4 && len(upper) != 6 && len(upper) != 8) {
		return fmt.Errorf("grid square %q must be 4, 6, or 8 characters", s)
	}

	// Field: two letters A-R
	if upper[0] < 'A' || upper[0] > 'R' || upper[1] < 'A' || upper[1] > 'R' {
		return fmt.Errorf("grid square %q field letters must be A-R", s)
	}
	// Square: two digits 0-9
	if upper[2] < '0' || upper[2] > '9' || upper[3] < '0' || upper[3] > '9' {
		return fmt.Errorf("grid square %q square digits must be 0-9", s)
	}

	if len(upper) >= 6 {
		// Subsquare: two letters A-X
		if upper[4] < 'A' || upper[4] > 'X' || upper[5] < 'A' || upper[5] > 'X' {
			return fmt.Errorf("grid square %q subsquare letters must be A-X", s)
		}
	}

	return nil
}
