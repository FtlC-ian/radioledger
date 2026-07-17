package adif_test

import (
	"testing"

	"github.com/FtlC-ian/radioledger/pkg/adif"
)

func TestValidateDate(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"20260228", false},
		{"19451008", false}, // Historical QSO
		{"20260229", true},  // Not a leap year
		{"20240229", false}, // Leap year
		{"00000101", true},  // Year too early
		{"20261301", true},  // Invalid month
		{"20260132", true},  // Invalid day
		{"2026022", true},   // Too short
		{"2026022X", true},  // Non-digit
		{"", true},          // Empty
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateDate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDate(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateTime(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"1530", false},
		{"153000", false},
		{"0000", false},
		{"2359", false},
		{"235959", false},
		{"2400", true},   // Invalid hour
		{"1560", true},   // Invalid minute
		{"153060", true}, // Invalid second
		{"153", true},    // Too short
		{"15300", true},  // 5 chars (neither 4 nor 6)
		{"", true},       // Empty
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTime(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFrequency(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"14.225", false},
		{"7.074500", false},
		{"0.001", false},
		{"144.000", false},
		{"28.0", false},
		{"0.0", true},    // Zero
		{"-1.0", true},   // Negative
		{"999999", true}, // Way too high
		{"abc", true},    // Non-numeric
		{"", true},       // Empty
		{"0.0001", true}, // Below minimum
		{"300001", true}, // Above max
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateFrequency(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFrequency(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBand(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"20m", false},
		{"40m", false},
		{"2m", false},
		{"70cm", false},
		{"160m", false},
		{"6m", false},
		{"invalid", true},
		{"3m", true},
		{"", true},
		{"20M", false}, // case-insensitive
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateBand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBand(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateMode(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"SSB", false},
		{"CW", false},
		{"FT8", false},
		{"FT4", false},
		{"RTTY", false},
		{"FM", false},
		{"AM", false},
		{"WSPR", false},
		{"JT65", false},
		{"OFDM", false},
		{"FSK", false},
		{"MTONE", false},
		{"DYNAMIC", false},
		{"C4FM", false},
		{"PSK31", false},
		{"VARAHF", false},
		{"ft8", false}, // case-insensitive
		{"INVALID_MODE", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateMode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMode(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateCallsign(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"W1AW", false},
		{"K5XX", false},
		{"VE3XYZ", false},
		{"DL1ABC", false},
		{"JA1TEST", false},
		{"VK9/W5XXX", false},            // With prefix override
		{"W5XXX/P", false},              // With suffix
		{"W5XXX/MM", false},             // Maritime mobile
		{"3W3RR", false},                // Numeric prefix
		{"", true},                      // Empty
		{"TOOLONGCALLSIGN123456", true}, // Too long
		{"NODIGITS", true},              // No digit
		{"1234567", true},               // No letter
		{"W1 AW", true},                 // Space (invalid)
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateCallsign(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCallsign(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateGridsquare(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"EM35", false},
		{"EM35ab", false},
		{"DM79", false},
		{"IO91", false},
		{"QF89", false},
		{"em35", false},     // lowercase accepted (normalized internally)
		{"EM", true},        // Too short
		{"EM35ab12", false}, // 8-char extended
		{"ZZ00", true},      // Invalid field letters (Z > R)
		{"", true},          // Empty
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := adif.ValidateGridsquare(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGridsquare(%q) = %v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecord(t *testing.T) {
	rec := &adif.Record{
		Fields: []adif.Field{
			{Name: "CALL", Value: "W1AW"},
			{Name: "BAND", Value: "20m"},
			{Name: "MODE", Value: "SSB"},
			{Name: "QSO_DATE", Value: "20260228"},
			{Name: "TIME_ON", Value: "153000"},
			{Name: "FREQ", Value: "14.225"},
		},
	}
	result := adif.ValidateRecord(rec)
	if !result.OK() {
		t.Errorf("expected valid record, got errors: %v", result.Errors)
	}
}

func TestValidateRecordWithErrors(t *testing.T) {
	rec := &adif.Record{
		Fields: []adif.Field{
			{Name: "CALL", Value: "W1AW"},
			{Name: "BAND", Value: "invalid_band"},
			{Name: "QSO_DATE", Value: "99999999"},
		},
	}
	result := adif.ValidateRecord(rec)
	if result.OK() {
		t.Error("expected validation errors, got none")
	}
	if len(result.Errors) < 2 {
		t.Errorf("expected at least 2 errors, got %d", len(result.Errors))
	}
}

func TestValidateFieldAPPNamespace(t *testing.T) {
	// APP_* fields should not be validated (pass through)
	errs := adif.ValidateField("APP_WSJT_SNR", "-15")
	if len(errs) > 0 {
		t.Errorf("APP_* field should not be validated, got: %v", errs)
	}
}

func TestValidateRST599(t *testing.T) {
	// RST "599" is widely used for FT8 even though it's technically wrong.
	// We must not reject it. RST fields are currently not validated by type.
	rec := &adif.Record{
		Fields: []adif.Field{
			{Name: "RST_SENT", Value: "599"},
			{Name: "RST_RCVD", Value: "599"},
		},
	}
	result := adif.ValidateRecord(rec)
	if !result.OK() {
		t.Errorf("FT8 599 RST should be accepted, got errors: %v", result.Errors)
	}
}
