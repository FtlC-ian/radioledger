// Package adif implements a strict ADIF (Amateur Data Interchange Format) parser.
// ADIF fields use the format: <FIELDNAME:length>data or <FIELDNAME:length:type>data
package adif

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Record is a map of ADIF field names (uppercased) to their values.
type Record map[string]string

// ParseError describes a syntax error found while parsing ADIF input.
type ParseError struct {
	// Offset is the byte position in the input where the error was detected.
	Offset int
	// Field is the field name being parsed when the error occurred (may be empty).
	Field string
	// Msg describes the error.
	Msg string
}

func (e *ParseError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("adif parse error at offset %d (field %s): %s", e.Offset, e.Field, e.Msg)
	}
	return fmt.Sprintf("adif parse error at offset %d: %s", e.Offset, e.Msg)
}

// Parser holds the state for an ADIF parser.
type Parser struct {
	input string
	pos   int
}

// NewParser creates a new ADIF parser from raw ADIF text.
func NewParser(input string) *Parser {
	return &Parser{input: input, pos: 0}
}

// parseTag reads the next <FIELD:len> or <FIELD:len:type> tag and returns the
// field name, field value, and type specifier (empty if absent).
//
// Returns ("", "", "", io.EOF) when there are no more '<' delimiters in the input.
// Returns ("", "", "", *ParseError) for any malformed tag.
func (p *Parser) parseTag() (name, value, typSpec string, err error) {
	// Scan forward for '<'
	start := strings.Index(p.input[p.pos:], "<")
	if start < 0 {
		return "", "", "", io.EOF
	}
	tagStart := p.pos + start
	p.pos = tagStart + 1 // skip '<'

	end := strings.Index(p.input[p.pos:], ">")
	if end < 0 {
		return "", "", "", &ParseError{
			Offset: tagStart,
			Msg:    "unterminated tag (no closing '>')",
		}
	}
	tag := p.input[p.pos : p.pos+end]
	p.pos += end + 1 // skip '>'

	parts := strings.SplitN(tag, ":", 3)
	name = strings.ToUpper(strings.TrimSpace(parts[0]))
	if name == "" {
		return "", "", "", &ParseError{
			Offset: tagStart,
			Msg:    fmt.Sprintf("empty field name in tag <%s>", tag),
		}
	}

	// Tags like <EOH> and <EOR> have no length part.
	if len(parts) == 1 {
		return name, "", "", nil
	}

	// Parse length — must be a non-negative integer.
	// Use strconv.Atoi to strictly reject partial matches like "1.5" or "1abc".
	lenStr := strings.TrimSpace(parts[1])
	length, parseErr := strconv.Atoi(lenStr)
	if parseErr != nil {
		return "", "", "", &ParseError{
			Offset: tagStart,
			Field:  name,
			Msg:    fmt.Sprintf("non-numeric length %q", lenStr),
		}
	}

	if length < 0 {
		return "", "", "", &ParseError{
			Offset: tagStart,
			Field:  name,
			Msg:    fmt.Sprintf("negative length %d", length),
		}
	}

	if p.pos+length > len(p.input) {
		return "", "", "", &ParseError{
			Offset: tagStart,
			Field:  name,
			Msg: fmt.Sprintf(
				"declared length %d exceeds remaining input (%d bytes available)",
				length, len(p.input)-p.pos,
			),
		}
	}

	if len(parts) == 3 {
		typSpec = strings.TrimSpace(parts[2])
	}

	value = strings.TrimRight(p.input[p.pos:p.pos+length], " \t\r\n")
	p.pos += length
	return name, value, typSpec, nil
}

// NextRecord returns the next complete ADIF record (terminated by <EOR>).
// Returns (nil, nil) when there are no more records in the input.
// Returns (nil, *ParseError) for any malformed tag encountered.
// Returns (rec, fmt.Errorf(...)) if end of input is reached without <EOR> and
// a partial record has been accumulated.
func (p *Parser) NextRecord() (Record, error) {
	rec := make(Record)
	for {
		name, value, _, err := p.parseTag()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(rec) > 0 {
					return nil, fmt.Errorf("incomplete ADIF record: reached end of input without <EOR> (accumulated fields: %d)", len(rec))
				}
				return nil, nil // clean EOF — no more records
			}
			return nil, err
		}
		switch name {
		case "EOH":
			// End of header — discard any accumulated header fields and continue.
			rec = make(Record)
		case "EOR":
			if len(rec) > 0 {
				return rec, nil
			}
			// Empty record (EOR with no fields) — keep going.
		default:
			rec[name] = value
		}
	}
}

// ParseAll parses all ADIF records from the input text, skipping the header.
// Returns an error on the first malformed tag or incomplete record; in that
// case the returned slice contains only the records successfully parsed before
// the error.
func ParseAll(input string) ([]Record, error) {
	p := NewParser(input)
	var records []Record
	for {
		rec, err := p.NextRecord()
		if err != nil {
			return records, err
		}
		if rec == nil {
			break
		}
		records = append(records, rec)
	}
	return records, nil
}

// EncodeField encodes a single ADIF field as <NAME:len>value.
func EncodeField(name, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("<%s:%d>%s\n", name, len(value), value)
}

// EncodeFieldWithType encodes a field with a type specifier <NAME:len:type>value.
func EncodeFieldWithType(name, value, typSpec string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("<%s:%d:%s>%s\n", name, len(value), typSpec, value)
}
