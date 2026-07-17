package callsign

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	pdf "github.com/ledongthuc/pdf"
)

const (
	// BNetzAFullDumpURL is the official Bundesnetzagentur amateur radio callsign list PDF.
	BNetzAFullDumpURL = "https://data.bundesnetzagentur.de/Bundesnetzagentur/SharedDocs/Downloads/DE/Sachgebiete/Telekommunikation/Unternehmen_Institutionen/Frequenzen/Amateurfunk/Rufzeichenliste/rufzeichenliste_afu.pdf"

	bnetzaSource = "bnetza"
)

var (
	bnetzaRecordStartRe = regexp.MustCompile(`^(D[A-R][0-9][A-Z0-9]{1,7})\s*,\s*([^,]+?)\s*,\s*(.+)$`)
	bnetzaPostalCityRe  = regexp.MustCompile(`\b(\d{5})\s+([^,;]+)`)
)

// ParseBNetzA downloads and parses the German BNetzA callsign PDF.
func ParseBNetzA(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "bnetza_parser: downloading PDF", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "bnetza_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseBNetzAData(ctx, data)
}

// ParseBNetzAData parses BNetzA PDF bytes already loaded in memory.
func ParseBNetzAData(ctx context.Context, data []byte) (*ParseResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty PDF payload")
	}

	text, err := extractBNetzAText(ctx, data)
	if err != nil {
		return nil, err
	}

	result := parseBNetzAText(text)
	slog.InfoContext(ctx, "bnetza_parser: parsed records",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)
	return result, nil
}

func extractBNetzAText(ctx context.Context, data []byte) (string, error) {
	text, err := extractBNetzATextWithPDFToText(ctx, data)
	if err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}

	slog.WarnContext(ctx, "bnetza_parser: pdftotext extraction failed; falling back to Go PDF parser",
		slog.String("error", errString(err)),
	)

	fallbackText, fallbackErr := extractBNetzATextWithGoPDF(data)
	if fallbackErr != nil {
		return "", fmt.Errorf("extract PDF text (pdftotext failed: %v, go parser failed: %w)", err, fallbackErr)
	}
	if strings.TrimSpace(fallbackText) == "" {
		return "", fmt.Errorf("PDF text extraction produced empty output")
	}
	return fallbackText, nil
}

func extractBNetzATextWithPDFToText(ctx context.Context, data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "bnetza-pdf-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pdfPath := filepath.Join(tmpDir, "rufzeichenliste_afu.pdf")
	if err := os.WriteFile(pdfPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write temp pdf: %w", err)
	}

	cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", "-nopgbrk", pdfPath, "-")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("pdftotext: %w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("pdftotext: %w", err)
	}

	return strings.ToValidUTF8(string(out), ""), nil
}

func extractBNetzATextWithGoPDF(data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "bnetza-pdf-go-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	pdfPath := filepath.Join(tmpDir, "rufzeichenliste_afu.pdf")
	if err := os.WriteFile(pdfPath, data, 0o600); err != nil {
		return "", fmt.Errorf("write temp pdf: %w", err)
	}

	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _ = f.Close() }()

	reader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("get plain text: %w", err)
	}

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, reader); err != nil {
		return "", fmt.Errorf("read plain text: %w", err)
	}

	return strings.ToValidUTF8(buf.String(), ""), nil
}

func parseBNetzAText(text string) *ParseResult {
	result := &ParseResult{}

	var current *bnetzaEntryBuilder

	flush := func() {
		if current == nil {
			return
		}
		result.Processed++
		norm := current.normalize()
		if norm == nil {
			result.Skipped++
		} else {
			result.Records = append(result.Records, *norm)
		}
		current = nil
	}

	for _, rawLine := range strings.Split(text, "\n") {
		line := collapseWhitespace(rawLine)
		if line == "" {
			continue
		}

		if isBNetzANoiseLine(line) {
			continue
		}

		if m := bnetzaRecordStartRe.FindStringSubmatch(line); m != nil {
			flush()
			current = &bnetzaEntryBuilder{
				callsign: strings.ToUpper(strings.TrimSpace(m[1])),
				class:    strings.TrimSpace(m[2]),
				parts:    []string{strings.TrimSpace(m[3])},
			}
			continue
		}

		if current != nil {
			current.parts = append(current.parts, line)
		}
	}

	flush()
	return result
}

type bnetzaEntryBuilder struct {
	callsign string
	class    string
	parts    []string
}

func (b *bnetzaEntryBuilder) normalize() *NormalizedRecord {
	if b == nil || b.callsign == "" {
		return nil
	}

	joined := collapseWhitespace(strings.Join(b.parts, " "))
	if joined == "" {
		return nil
	}

	name := joined
	address := ""
	if semicolon := strings.Index(joined, ";"); semicolon >= 0 {
		name = strings.TrimSpace(joined[:semicolon])
		address = strings.TrimSpace(joined[semicolon+1:])
	}
	if name == "" {
		return nil
	}

	postalCode := ""
	city := ""
	candidate := joined
	if address != "" {
		candidate = address
	}
	if m := bnetzaPostalCityRe.FindStringSubmatch(candidate); len(m) == 3 {
		postalCode = strings.TrimSpace(m[1])
		city = normalizeBNetzACity(m[2])
	}

	licenseClass := normalizeBNetzALicenseClass(b.class)

	return &NormalizedRecord{
		Callsign:     b.callsign,
		Source:       bnetzaSource,
		FullName:     name,
		AddressLine1: address,
		PostalCode:   postalCode,
		City:         city,
		Country:      "Germany",
		LicenseClass: licenseClass,
		Status:       "active",
	}
}

func normalizeBNetzALicenseClass(raw string) string {
	raw = strings.ToLower(collapseWhitespace(raw))
	switch raw {
	case "a":
		return "a"
	case "e":
		return "e"
	default:
		return ""
	}
}

func normalizeBNetzACity(s string) string {
	s = strings.TrimSpace(strings.Trim(s, ",;"))
	s = collapseWhitespace(s)
	return strings.ToValidUTF8(s, "")
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(strings.TrimSpace(s), "")), " ")
}

func isBNetzANoiseLine(line string) bool {
	if line == "" {
		return true
	}
	if strings.HasPrefix(line, "Seite ") {
		return true
	}
	if strings.HasPrefix(line, "\f") {
		return true
	}
	if allDigits(line) {
		return true
	}
	if strings.HasPrefix(line, "Liste der ") || strings.HasPrefix(line, "Weitere Rufzeichen") || strings.HasPrefix(line, "Amateurfunkgesetzes") {
		return true
	}
	return false
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
