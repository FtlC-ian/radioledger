package adif

import "strings"

// Field represents a single ADIF field with a name, value, and optional type indicator.
// Field names are always stored in uppercase.
type Field struct {
	Name  string // Uppercase-normalized ADIF field name (e.g., "CALL", "BAND")
	Value string // Raw string value from the ADIF file
	Type  string // ADIF data type indicator (optional, e.g., "S", "N", "D")
}

// Record holds an ordered slice of ADIF fields from a single QSO record.
// Field order from the source file is preserved for semantic-lossless round-trips.
type Record struct {
	Fields []Field
}

// Get returns the value of the first field matching name (case-insensitive).
// Returns empty string if the field is not found.
func (r *Record) Get(name string) string {
	name = strings.ToUpper(name)
	for _, f := range r.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

// GetField returns the Field struct for the first field matching name (case-insensitive).
// The second return value is false if the field is not found.
func (r *Record) GetField(name string) (Field, bool) {
	name = strings.ToUpper(name)
	for _, f := range r.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Has returns true if a field with the given name exists (case-insensitive).
func (r *Record) Has(name string) bool {
	name = strings.ToUpper(name)
	for _, f := range r.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// Set sets the value of the first field with the given name. If no such field exists,
// a new field is appended. Name is normalized to uppercase.
func (r *Record) Set(name, value string) {
	name = strings.ToUpper(name)
	for i, f := range r.Fields {
		if f.Name == name {
			r.Fields[i].Value = value
			return
		}
	}
	r.Fields = append(r.Fields, Field{Name: name, Value: value})
}

// Delete removes all fields matching name (case-insensitive).
// Returns the number of fields removed.
func (r *Record) Delete(name string) int {
	name = strings.ToUpper(name)
	out := r.Fields[:0]
	removed := 0
	for _, f := range r.Fields {
		if f.Name == name {
			removed++
		} else {
			out = append(out, f)
		}
	}
	r.Fields = out
	return removed
}

// Clone returns a deep copy of the record.
func (r *Record) Clone() Record {
	fields := make([]Field, len(r.Fields))
	copy(fields, r.Fields)
	return Record{Fields: fields}
}

// Header holds the parsed ADIF file header fields.
// These appear before the <EOH> marker.
type Header struct {
	Fields []Field
}

// Get returns the value of the first header field matching name (case-insensitive).
func (h *Header) Get(name string) string {
	name = strings.ToUpper(name)
	for _, f := range h.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

// ADIFVersion returns the ADIF_VER field value from the header, or empty string.
func (h *Header) ADIFVersion() string {
	return h.Get("ADIF_VER")
}

// ProgramID returns the PROGRAMID field value from the header, or empty string.
func (h *Header) ProgramID() string {
	return h.Get("PROGRAMID")
}

// ProgramVersion returns the PROGRAMVERSION field value from the header.
func (h *Header) ProgramVersion() string {
	return h.Get("PROGRAMVERSION")
}
