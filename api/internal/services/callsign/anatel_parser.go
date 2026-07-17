package callsign

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const (
	// ANATELFullDumpURL is the ANATEL (Agência Nacional de Telecomunicações) CSV export
	// for Brazilian amateur radio (radioamador) licensees, published via SGCO (Sistema de
	// Gestão de Certificação e Outorga).
	ANATELFullDumpURL = "https://sistemas.anatel.gov.br/sgco/public/view/b/licenciamento.php?acao=export_csv&servico=010"

	anatelSource = "anatel"
)

// ParseANATELCSV downloads and parses the ANATEL radioamador CSV.
func ParseANATELCSV(ctx context.Context, url string) (*ParseResult, error) {
	slog.InfoContext(ctx, "anatel_parser: downloading csv", slog.String("url", url))

	data, err := downloadZip(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	slog.InfoContext(ctx, "anatel_parser: download complete",
		slog.String("url", url),
		slog.Int("bytes", len(data)),
	)

	return ParseANATELCSVData(ctx, data)
}

// ParseANATELCSVData parses ANATEL radioamador CSV bytes already loaded in memory.
//
// ANATEL publishes the data as a semicolon- or comma-delimited CSV, potentially
// encoded as ISO-8859-1 (Latin-1) due to Portuguese characters.  This function
// handles both encodings and both delimiters transparently.
//
// Expected columns (exact names may vary between releases):
//
//	indicativo           – callsign (e.g. PY2ABC)
//	nome                 – full name / razão social
//	uf                   – 2-letter state code (e.g. SP, RJ)
//	municipio            – city / municipality
//	categoria / classe   – license class (Classe A / B / C)
//	data_inicio / emissao – grant date
//	data_fim / validade  – expiry date
//	situacao             – status (ativo, cancelado, …)
func ParseANATELCSVData(ctx context.Context, data []byte) (*ParseResult, error) {
	decoded := decodeANATELText(data)
	if looksLikeHTML(decoded) {
		return nil, fmt.Errorf("downloaded payload looks like HTML, not CSV")
	}

	r := csv.NewReader(bytes.NewReader(decoded))
	r.Comma = detectCSVDelimiter(decoded)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	idx := anatelHeaderIndex(header)
	if anatelFindColumn(idx, "indicativo", "callsign", "call_sign", "indicativo_de_chamada") == -1 {
		return nil, fmt.Errorf("missing required callsign column (got: %v)", header)
	}

	result := &ParseResult{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip malformed rows rather than aborting the whole import.
			slog.WarnContext(ctx, "anatel_parser: skipping malformed row", slog.String("error", err.Error()))
			result.Skipped++
			continue
		}

		result.Processed++
		norm := normalizeANATELRow(row, idx)
		if norm == nil {
			result.Skipped++
			continue
		}
		result.Records = append(result.Records, *norm)
	}

	slog.InfoContext(ctx, "anatel_parser: parsed rows",
		slog.Int("processed", result.Processed),
		slog.Int("skipped", result.Skipped),
		slog.Int("records", len(result.Records)),
	)

	return result, nil
}

// normalizeANATELRow converts a single CSV row into a NormalizedRecord.
func normalizeANATELRow(row []string, idx map[string]int) *NormalizedRecord {
	callsign := strings.ToUpper(strings.TrimSpace(anatelField(row, idx,
		"indicativo", "callsign", "call_sign", "indicativo_de_chamada", "indicativo_de_chamada_da_estacao")))
	if !isLikelyCallsign(callsign) {
		return nil
	}

	fullName := strings.TrimSpace(anatelField(row, idx,
		"nome", "nome_do_titular", "titular", "razao_social", "nome_completo",
		"nome_do_licenciado", "licenciado"))

	state := strings.ToUpper(strings.TrimSpace(anatelField(row, idx,
		"uf", "estado", "sigla_uf", "estado_uf", "unidade_da_federacao")))

	city := strings.TrimSpace(anatelField(row, idx,
		"municipio", "cidade", "localidade", "municipio_do_titular"))

	licenseClass := normalizeANATELLicenseClass(anatelField(row, idx,
		"categoria", "classe", "classe_de_radioamador", "categoria_de_radioamador",
		"categoria_da_licenca", "classe_da_licenca"))

	sourceID := anatelField(row, idx,
		"numero_fistel", "fistel", "num_fistel", "fistel_estacao",
		"numero_processo", "processo", "id", "numero_autorizacao")

	grantDate := parseANATELDate(anatelField(row, idx,
		"data_inicio_vigencia", "data_inicio", "data_emissao", "data_de_emissao",
		"data_expedicao", "data_de_expedicao", "data_outorga", "inicio_vigencia",
		"data_inicio_de_vigencia"))

	expiryDate := parseANATELDate(anatelField(row, idx,
		"data_fim_vigencia", "data_fim", "data_validade", "data_de_validade",
		"vencimento", "data_vencimento", "data_expiracao", "fim_vigencia",
		"data_fim_de_vigencia"))

	statusRaw := anatelField(row, idx,
		"situacao", "status", "situacao_outorga", "situacao_da_licenca")

	return &NormalizedRecord{
		Callsign:      callsign,
		Source:        anatelSource,
		SourceID:      sourceID,
		FullName:      fullName,
		StateProvince: state,
		City:          city,
		Country:       "Brazil",
		LicenseClass:  licenseClass,
		GrantDate:     grantDate,
		ExpiryDate:    expiryDate,
		Status:        normalizeANATELStatus(statusRaw),
	}
}

// normalizeANATELLicenseClass maps ANATEL's Portuguese class labels to
// lowercase underscore-separated tokens consistent with other parsers.
//
// ANATEL uses three amateur radio license classes:
//   - Classe A (Advanced) — full privileges
//   - Classe B (General)  — intermediate privileges
//   - Classe C (Novice)   — entry level
func normalizeANATELLicenseClass(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	switch {
	case strings.Contains(s, "classe a") || s == "a":
		return "class_a"
	case strings.Contains(s, "classe b") || s == "b":
		return "class_b"
	case strings.Contains(s, "classe c") || s == "c":
		return "class_c"
	default:
		// Return the raw value cleaned up so we don't silently discard novel values.
		s = strings.NewReplacer(
			"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
			"é", "e", "è", "e", "ê", "e", "ë", "e",
			"í", "i", "ì", "i", "î", "i", "ï", "i",
			"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
			"ú", "u", "ù", "u", "û", "u", "ü", "u",
			"ç", "c", "ñ", "n",
			" ", "_", "-", "_", "/", "_",
		).Replace(s)
		parts := strings.FieldsFunc(s, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_'
		})
		return strings.Join(parts, "_")
	}
}

// normalizeANATELStatus maps Portuguese status strings to "active" or "expired".
func normalizeANATELStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "cancelado", "cancelada", "expirado", "expirada", "vencido", "vencida",
		"cassado", "cassada", "encerrado", "encerrada", "revogado", "revogada",
		"suspenso", "suspensa":
		return "expired"
	default:
		// "ativo", "ativa", "outorgado", "vigente", or empty → assume active.
		return "active"
	}
}

// parseANATELDate parses common Brazilian date formats.
func parseANATELDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Try ISO first, then Brazilian DD/MM/YYYY, then other variants.
	layouts := []string{
		"2006-01-02",
		"02/01/2006",
		"02-01-2006",
		"02.01.2006",
		"2006/01/02",
		"01/02/2006", // US-style occasionally seen
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// ── Header helpers ────────────────────────────────────────────────────────────

func anatelField(row []string, idx map[string]int, aliases ...string) string {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeANATELHeader(alias)]; ok && i >= 0 && i < len(row) {
			return strings.ToValidUTF8(strings.TrimSpace(row[i]), "")
		}
	}
	return ""
}

func anatelHeaderIndex(header []string) map[string]int {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[canonicalizeANATELHeader(h)] = i
	}
	return idx
}

func anatelFindColumn(idx map[string]int, aliases ...string) int {
	for _, alias := range aliases {
		if i, ok := idx[canonicalizeANATELHeader(alias)]; ok {
			return i
		}
	}
	return -1
}

// canonicalizeANATELHeader normalises a Portuguese column header for fuzzy matching:
// lower-cases, strips diacritics, and collapses punctuation/spaces to underscores.
func canonicalizeANATELHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u",
		"ç", "c", "ñ", "n",
		"'", " ", "'", " ", "-", " ", ".", " ",
	).Replace(s)
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	return strings.Join(parts, "_")
}

// decodeANATELText converts ISO-8859-1 / Latin-1 encoded ANATEL data to UTF-8,
// or returns the input unchanged if it is already valid UTF-8.
func decodeANATELText(data []byte) []byte {
	if utf8.Valid(data) {
		return bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // strip BOM
	}
	decoded, err := charmap.ISO8859_1.NewDecoder().Bytes(data)
	if err != nil {
		return data
	}
	return bytes.TrimPrefix(decoded, []byte("\xef\xbb\xbf"))
}
