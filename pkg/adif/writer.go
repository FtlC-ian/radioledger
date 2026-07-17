package adif

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	// ADIFVersion is the ADIF spec version this writer targets.
	ADIFVersion = "3.1.7"

	// ProgramID is the PROGRAMID header field value.
	ProgramID = "RadioLedger"
)

// WriterOptions configures the ADIF writer.
type WriterOptions struct {
	// ProgramVersion is included in the ADIF header PROGRAMVERSION field.
	ProgramVersion string

	// FieldsPerLine controls how many fields are written per line (0 = one per line).
	FieldsPerLine int

	// IncludeHeader controls whether the ADIF header is written. Default: true.
	IncludeHeader bool
}

// Writer writes ADIF ADI format to an io.Writer.
// Output is deterministic: field ordering is canonical for consistent exports.
type Writer struct {
	w    io.Writer
	opts WriterOptions

	headerWritten bool
}

// NewWriter creates a new ADIF writer with default options.
func NewWriter(w io.Writer) *Writer {
	return NewWriterWithOptions(w, WriterOptions{
		ProgramVersion: "1.0.0",
		IncludeHeader:  true,
		FieldsPerLine:  0,
	})
}

// NewWriterWithOptions creates a new ADIF writer with custom options.
func NewWriterWithOptions(w io.Writer, opts WriterOptions) *Writer {
	if opts.ProgramVersion == "" {
		opts.ProgramVersion = "1.0.0"
	}
	return &Writer{w: w, opts: opts}
}

// WriteHeader writes the ADIF file header with standard RadioLedger fields.
// If extra header fields are provided, they are included after the standard fields.
// WriteHeader is called automatically by WriteRecord if not called explicitly.
func (w *Writer) WriteHeader(extraFields ...Field) error {
	if w.headerWritten {
		return nil
	}
	w.headerWritten = true

	if !w.opts.IncludeHeader {
		return nil
	}

	// Standard header fields
	standardFields := []Field{
		{Name: "ADIF_VER", Value: ADIFVersion},
		{Name: "PROGRAMID", Value: ProgramID},
		{Name: "PROGRAMVERSION", Value: w.opts.ProgramVersion},
	}

	// Write pre-header comment (some programs expect text before the first tag)
	_, err := fmt.Fprintf(w.w, "RadioLedger ADIF Export\n")
	if err != nil {
		return fmt.Errorf("adif writer: writing preamble: %w", err)
	}

	for _, f := range standardFields {
		if err := w.writeField(f); err != nil {
			return err
		}
	}
	for _, f := range extraFields {
		if err := w.writeField(f); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(w.w, "<EOH>\n\n")
	return err
}

// WriteRecord writes a single QSO record in canonical ADIF field order.
// WriteHeader is called automatically if not already done.
func (w *Writer) WriteRecord(rec *Record) error {
	if !w.headerWritten {
		if err := w.WriteHeader(); err != nil {
			return err
		}
	}

	normalized := rec.Clone()
	CanonicalizeRecordMode(&normalized)

	// Sort fields into canonical order
	ordered := sortFields(normalized.Fields)

	for _, f := range ordered {
		if err := w.writeField(f); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintf(w.w, "<EOR>\n")
	return err
}

// WriteRecords writes multiple QSO records. Calls WriteHeader if needed.
func (w *Writer) WriteRecords(records []*Record) error {
	for _, rec := range records {
		if err := w.WriteRecord(rec); err != nil {
			return err
		}
	}
	return nil
}

// writeField writes a single ADIF field tag + value to the output.
func (w *Writer) writeField(f Field) error {
	value := f.Value
	length := len(value)

	var tag string
	if f.Type != "" {
		tag = fmt.Sprintf("<%s:%d:%s>%s", f.Name, length, f.Type, value)
	} else {
		tag = fmt.Sprintf("<%s:%d>%s", f.Name, length, value)
	}

	_, err := fmt.Fprintf(w.w, "%s\n", tag)
	return err
}

// sortFields returns a copy of fields sorted into canonical ADIF export order.
// Fields in CanonicalFieldOrder come first (in that order); remaining fields
// are sorted alphabetically after. APP_* fields always go last.
func sortFields(fields []Field) []Field {
	// Build a position map from the canonical order
	posMap := make(map[string]int, len(CanonicalFieldOrder))
	for i, name := range CanonicalFieldOrder {
		posMap[name] = i
	}

	type indexedField struct {
		f       Field
		pos     int
		isApp   bool
		isKnown bool
	}

	indexed := make([]indexedField, len(fields))
	for i, f := range fields {
		pos, known := posMap[f.Name]
		isApp := strings.HasPrefix(f.Name, "APP_")
		indexed[i] = indexedField{
			f:       f,
			pos:     pos,
			isApp:   isApp,
			isKnown: known,
		}
	}

	sort.SliceStable(indexed, func(i, j int) bool {
		a, b := indexed[i], indexed[j]

		// APP_* fields always go last
		if a.isApp != b.isApp {
			return !a.isApp
		}
		if a.isApp && b.isApp {
			return a.f.Name < b.f.Name
		}

		// Both are canonical known fields
		if a.isKnown && b.isKnown {
			return a.pos < b.pos
		}
		// Known before unknown
		if a.isKnown != b.isKnown {
			return a.isKnown
		}
		// Both unknown: alphabetical
		return a.f.Name < b.f.Name
	})

	result := make([]Field, len(indexed))
	for i, item := range indexed {
		result[i] = item.f
	}
	return result
}

// FormatRecord returns the ADIF representation of a single record as a string.
// Useful for debugging and testing.
func FormatRecord(rec *Record) string {
	var sb strings.Builder
	w := NewWriterWithOptions(&sb, WriterOptions{IncludeHeader: false})
	_ = w.WriteRecord(rec)
	return sb.String()
}

// FormatAll writes all records to a string, including the ADIF header.
func FormatAll(header *Header, records []*Record, programVersion string) (string, error) {
	var sb strings.Builder
	w := NewWriterWithOptions(&sb, WriterOptions{
		ProgramVersion: programVersion,
		IncludeHeader:  true,
	})

	var extraFields []Field
	if header != nil {
		for _, f := range header.Fields {
			upper := strings.ToUpper(f.Name)
			// Skip the standard header fields — we write those ourselves
			if upper == "ADIF_VER" || upper == "PROGRAMID" || upper == "PROGRAMVERSION" {
				continue
			}
			extraFields = append(extraFields, f)
		}
	}

	if err := w.WriteHeader(extraFields...); err != nil {
		return "", err
	}
	if err := w.WriteRecords(records); err != nil {
		return "", err
	}
	return sb.String(), nil
}
