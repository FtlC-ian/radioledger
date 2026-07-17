package adif

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

const (
	// DefaultMaxFieldLen is the default maximum byte length for a single field value.
	// Fields claiming to be larger than this are rejected as potentially malicious.
	DefaultMaxFieldLen = 10 * 1024 * 1024 // 10 MB

	// DefaultMaxRecords is the default maximum number of QSO records per file.
	DefaultMaxRecords = 500_000
)

// ParseError is returned when the parser encounters invalid ADIF syntax.
type ParseError struct {
	Offset  int64  // approximate byte offset in source
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("adif parse error at offset %d: %s", e.Offset, e.Message)
}

// ParserOptions configures the streaming ADIF parser.
type ParserOptions struct {
	// MaxFieldLen is the maximum allowed byte length for a single field value.
	// Values larger than this are rejected. Default: DefaultMaxFieldLen (10 MB).
	MaxFieldLen int64

	// MaxRecords is the maximum number of QSO records the parser will return.
	// Exceeding this limit returns ErrTooManyRecords. Default: DefaultMaxRecords.
	MaxRecords int
}

// ErrTooManyRecords is returned when MaxRecords is exceeded.
var ErrTooManyRecords = fmt.Errorf("adif: record count exceeds configured maximum")

// Parser streams QSO records from an ADIF ADI file.
// It never loads the entire file into memory.
type Parser struct {
	r                *bufio.Reader
	opts             ParserOptions
	headerDone       bool
	header           *Header
	recordCount      int
	offset           int64
	prefetchedFields []Field // fields consumed during header scan when no EOH found
	hasPrefetch      bool
	atEOF            bool
}

// NewParser creates a new streaming ADI parser reading from r with default options.
func NewParser(r io.Reader) *Parser {
	return NewParserWithOptions(r, ParserOptions{})
}

// NewParserWithOptions creates a new parser with custom options.
func NewParserWithOptions(r io.Reader, opts ParserOptions) *Parser {
	if opts.MaxFieldLen <= 0 {
		opts.MaxFieldLen = DefaultMaxFieldLen
	}
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = DefaultMaxRecords
	}
	return &Parser{
		r:    bufio.NewReaderSize(r, 64*1024),
		opts: opts,
	}
}

// Header parses and returns the ADIF file header (fields before <EOH>).
// If there is no <EOH> marker, the header is empty and all content is treated as records.
// Header must be called before Next; calling it multiple times returns the cached result.
func (p *Parser) Header(ctx context.Context) (*Header, error) {
	if p.headerDone {
		return p.header, nil
	}
	p.headerDone = true

	// Detect and strip UTF-8 BOM
	bom, err := p.r.Peek(3)
	if err == nil && len(bom) == 3 && bom[0] == 0xEF && bom[1] == 0xBB && bom[2] == 0xBF {
		_, _ = p.r.Discard(3)
		p.offset += 3
	}

	h := &Header{}
	fields, marker, err := p.scanFields(ctx, true)
	if err != nil && err != io.EOF {
		return nil, err
	}

	if marker == markerEOH {
		// Normal header: fields before EOH.
		h.Fields = fields
		if err == io.EOF {
			p.atEOF = true
		}
	} else if marker == markerEOR {
		// EOR before EOH: the text before was a ADIF comment/preamble.
		// Treat the whole file as records; these fields are record 0's fields.
		h.Fields = nil
		p.prefetchedFields = fields
		p.hasPrefetch = len(fields) > 0
	} else {
		// EOF without any marker: no header, treat all as records.
		h.Fields = nil
		p.prefetchedFields = fields
		p.hasPrefetch = len(fields) > 0
		p.atEOF = true
	}

	p.header = h
	return h, nil
}

// Next returns the next QSO record from the file.
// Returns io.EOF when there are no more records.
// Returns ErrTooManyRecords if MaxRecords is exceeded.
// The context is checked between records for cancellation.
func (p *Parser) Next(ctx context.Context) (*Record, error) {
	if !p.headerDone {
		if _, err := p.Header(ctx); err != nil {
			return nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if p.atEOF && !p.hasPrefetch {
		return nil, io.EOF
	}

	if p.recordCount >= p.opts.MaxRecords {
		return nil, ErrTooManyRecords
	}

	// Loop to skip empty EOR records (produced by malformed input with no valid fields).
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var startFields []Field
		if p.hasPrefetch {
			startFields = p.prefetchedFields
			p.hasPrefetch = false
			p.prefetchedFields = nil
		}

		if p.atEOF {
			if len(startFields) == 0 {
				return nil, io.EOF
			}
			// Leftover fields from prefetch at EOF (no trailing EOR)
			p.recordCount++
			return &Record{Fields: startFields}, nil
		}

		fields, marker, err := p.scanFields(ctx, false)
		if err == io.EOF {
			p.atEOF = true
		} else if err != nil {
			return nil, err
		}

		allFields := append(startFields, fields...)

		switch marker {
		case markerEOR:
			// Normal record end
			if len(allFields) == 0 {
				// Empty EOR (garbage between records): skip and continue.
				if p.atEOF {
					return nil, io.EOF
				}
				continue
			}
			p.recordCount++
			return &Record{Fields: allFields}, nil

		case markerEOH:
			// Second EOH encountered (malformed file): treat as EOR
			if len(allFields) == 0 {
				if p.atEOF {
					return nil, io.EOF
				}
				continue
			}
			p.recordCount++
			return &Record{Fields: allFields}, nil

		case markerNone:
			// EOF without marker
			if len(allFields) == 0 {
				return nil, io.EOF
			}
			p.recordCount++
			return &Record{Fields: allFields}, nil
		}
	}
}

type endMarker int

const (
	markerNone endMarker = iota
	markerEOR
	markerEOH
)

// scanFields reads ADIF fields from the stream until a terminal marker (EOR or EOH)
// or EOF is reached. Returns the fields collected, the marker found, and any error.
//
// In header mode (isHeader=true), both EOR and EOH stop scanning.
// In record mode (isHeader=false), EOR stops scanning; EOH also stops.
func (p *Parser) scanFields(ctx context.Context, isHeader bool) ([]Field, endMarker, error) {
	var fields []Field

	for {
		if err := ctx.Err(); err != nil {
			return fields, markerNone, err
		}

		tag, err := p.findNextTag()
		if err == io.EOF {
			return fields, markerNone, io.EOF
		}
		if err != nil {
			return fields, markerNone, err
		}

		name, length, dataType, parseErr := parseTag(tag)
		if parseErr != nil {
			// Malformed tag: skip and continue scanning for next '<'
			continue
		}

		upper := strings.ToUpper(name)

		// Check for record/header delimiters
		if upper == "EOR" {
			return fields, markerEOR, nil
		}
		if upper == "EOH" {
			return fields, markerEOH, nil
		}

		// Enforce maximum field length
		if length > p.opts.MaxFieldLen {
			return nil, markerNone, &ParseError{
				Offset:  p.offset,
				Message: fmt.Sprintf("field %s claims length %d which exceeds maximum %d", name, length, p.opts.MaxFieldLen),
			}
		}

		// Read the field value
		value, readErr := p.readFieldValue(length)

		// Sanitize encoding
		value = sanitizeEncoding(value)

		if upper != "" {
			fields = append(fields, Field{
				Name:  upper,
				Value: value,
				Type:  dataType,
			})
		}

		if readErr == io.EOF {
			return fields, markerNone, io.EOF
		}
		if readErr != nil {
			return fields, markerNone, readErr
		}
	}
}

// findNextTag scans forward until '<', then reads until '>'.
// Returns the content between '<' and '>', or io.EOF.
func (p *Parser) findNextTag() (string, error) {
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			return "", err
		}
		p.offset++
		if b == '<' {
			content, err := p.r.ReadString('>')
			p.offset += int64(len(content))
			if err != nil && err != io.EOF {
				return "", err
			}
			// Strip trailing '>'
			if len(content) > 0 && content[len(content)-1] == '>' {
				content = content[:len(content)-1]
			}
			return content, nil
		}
	}
}

// parseTag parses "<FIELDNAME:LENGTH[:TYPE]>" content.
// EOR/EOH have no colon and are returned with length=0.
func parseTag(tag string) (string, int64, string, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", 0, "", fmt.Errorf("empty tag")
	}

	if !strings.Contains(tag, ":") {
		return tag, 0, "", nil
	}

	parts := strings.SplitN(tag, ":", 3)
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return "", 0, "", fmt.Errorf("empty field name in tag: %q", tag)
	}

	lengthStr := strings.TrimSpace(parts[1])
	length, err := strconv.ParseInt(lengthStr, 10, 64)
	if err != nil || length < 0 {
		return "", 0, "", fmt.Errorf("invalid length %q in tag %q", lengthStr, tag)
	}

	dataType := ""
	if len(parts) == 3 {
		dataType = strings.TrimSpace(parts[2])
	}

	return name, length, dataType, nil
}

// readFieldValue reads exactly n bytes from the reader as the field value.
func (p *Parser) readFieldValue(n int64) (string, error) {
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	nRead, err := io.ReadFull(p.r, buf)
	p.offset += int64(nRead)
	if err != nil {
		if err == io.ErrUnexpectedEOF {
			return string(buf[:nRead]), io.EOF
		}
		return string(buf[:nRead]), err
	}
	return string(buf), nil
}

// sanitizeEncoding converts non-UTF-8 content to valid UTF-8.
// Attempts Windows-1252 decoding (superset of Latin-1) for non-UTF-8 content.
func sanitizeEncoding(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	decoded, err := decodeWindows1252([]byte(s))
	if err == nil && utf8.ValidString(decoded) {
		return decoded
	}
	return strings.ToValidUTF8(s, "\uFFFD")
}

// decodeWindows1252 decodes Windows-1252 encoded bytes to UTF-8.
func decodeWindows1252(b []byte) (string, error) {
	decoder := charmap.Windows1252.NewDecoder()
	result, _, err := transform.Bytes(decoder, b)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

// ParseAll reads all records from r and returns them as a slice.
// Suitable for smaller files; for large imports use the streaming Parser directly.
func ParseAll(ctx context.Context, r io.Reader) (*Header, []*Record, error) {
	p := NewParser(r)
	header, err := p.Header(ctx)
	if err != nil {
		return nil, nil, err
	}

	var records []*Record
	for {
		rec, err := p.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return header, records, err
		}
		records = append(records, rec)
	}
	return header, records, nil
}

// ParseString parses an ADIF string and returns all records. Convenience function.
func ParseString(ctx context.Context, s string) (*Header, []*Record, error) {
	return ParseAll(ctx, strings.NewReader(s))
}

// ParseBytes parses ADIF from a byte slice. Convenience function for tests and fuzz.
func ParseBytes(ctx context.Context, b []byte) (*Header, []*Record, error) {
	return ParseAll(ctx, bytes.NewReader(b))
}
