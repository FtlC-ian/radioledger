// Package signer implements the TQSL signing logic required to build .tq8 files
// for submission to ARRL's Logbook of the World.
//
// .tq8 is a gzip-compressed ADIF-like text file with four sections:
//  1. TQSL_IDENT   – software/config version metadata
//  2. tCERT        – base64-encoded DER X.509 callsign certificate
//  3. tSTATION     – station location (grid, DXCC, etc.)
//  4. tCONTACT(s)  – individual QSO records, each with SIGNDATA and SIGN_LOTW_V2.0
//
// Signing algorithm (per ARRL spec confirmed by CloudLog's PHP implementation):
//
//	signdata_string = concat(station_fields... + qso_fields...)
//	signature       = base64(RSA_PKCS1v15_Sign(SHA1, signdata_string))
//
// Field ordering for signdata (station first, then QSO, per TQSL sigspec):
//
//	Station: CA_PROVINCE, CQZ, GRIDSQUARE, IOTA, ITUZ, US_COUNTY, US_STATE
//	Contact: BAND, BAND_RX, CALL, FREQ, FREQ_RX, MODE, PROP_MODE, QSO_DATE, QSO_TIME, SAT_NAME
package signer

import (
	"bytes"
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // SHA-1 required by ARRL LoTW spec — not our choice
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/adif"
)

const (
	// tqslIdent is the TQSL_IDENT header line written verbatim.
	tqslIdent = "TQSL V2.5.4 Lib: V2.5 Config: V11.12 AllowDupes: false"
)

// StationInfo contains station location fields included in both tSTATION and the sign string.
type StationInfo struct {
	Callsign   string
	DXCC       string // DXCC entity number string e.g. "291"
	Gridsquare string
	ITUZ       string
	CQZ        string
	IOTA       string
	// US-specific
	USState  string
	USCounty string
	// Canada-specific
	CAProvince string
	// Country name (used to decide which region fields to include)
	Country string
}

// QSO represents a single contact to be signed and included in the .tq8.
type QSO struct {
	Call     string
	Band     string
	BandRX   string
	Freq     string // MHz, e.g. "14.225"
	FreqRX   string
	Mode     string
	Submode  string
	PropMode string
	SatName  string
	QSODate  string // YYYY-MM-DD
	QSOTime  string // HH:MM:SSZ
}

// BuildTQ8 creates a gzip-compressed .tq8 file from the given certificate, station,
// and list of QSOs. The RSA private key is used for signing and is zeroed from memory
// after use.
//
// certDER is the DER-encoded X.509 certificate.
// privateKey is the signer's RSA private key.
func BuildTQ8(certDER []byte, privateKey *rsa.PrivateKey, station StationInfo, qsos []QSO) ([]byte, error) {
	defer zeroKey(privateKey)

	if len(qsos) == 0 {
		return nil, fmt.Errorf("no QSOs to sign")
	}

	var buf bytes.Buffer

	// ── Section 1: TQSL_IDENT ────────────────────────────────────────────────
	buf.WriteString(fmt.Sprintf("<TQSL_IDENT:%d>%s\n\n", len(tqslIdent), tqslIdent))

	// ── Section 2: tCERT ─────────────────────────────────────────────────────
	certB64 := base64.StdEncoding.EncodeToString(certDER)
	// CloudLog uses len(trimmed) + 1 for a trailing newline; we use the exact length.
	buf.WriteString("<Rec_Type:5>tCERT\n")
	buf.WriteString("<CERT_UID:1>1\n")
	buf.WriteString(fmt.Sprintf("<CERTIFICATE:%d>\n%s\n", len(certB64)+1, certB64))
	buf.WriteString("\n<EOR>\n\n")

	// ── Section 3: tSTATION ──────────────────────────────────────────────────
	buf.WriteString("<Rec_Type:8>tSTATION\n")
	buf.WriteString("<STATION_UID:1>1\n")
	buf.WriteString("<CERT_UID:1>1\n")
	buf.WriteString(adif.EncodeField("CALL", station.Callsign))
	buf.WriteString(adif.EncodeField("DXCC", station.DXCC))
	if station.Gridsquare != "" {
		buf.WriteString(adif.EncodeField("GRIDSQUARE", strings.ToUpper(station.Gridsquare)))
	}
	if station.ITUZ != "" {
		buf.WriteString(adif.EncodeField("ITUZ", station.ITUZ))
	}
	if station.CQZ != "" {
		buf.WriteString(adif.EncodeField("CQZ", station.CQZ))
	}
	if station.IOTA != "" {
		buf.WriteString(adif.EncodeField("IOTA", strings.ToUpper(station.IOTA)))
	}
	country := strings.ToUpper(station.Country)
	if station.CAProvince != "" && country == "CANADA" {
		buf.WriteString(adif.EncodeField("CA_PROVINCE", strings.ToUpper(station.CAProvince)))
	}
	if station.USState != "" && (country == "UNITED STATES OF AMERICA" || country == "USA" || country == "ALASKA" || country == "HAWAII") {
		buf.WriteString(adif.EncodeField("US_STATE", strings.ToUpper(station.USState)))
	}
	if station.USCounty != "" && (country == "UNITED STATES OF AMERICA" || country == "USA" || country == "ALASKA" || country == "HAWAII") {
		// Normalize to uppercase — must match buildSigndata which also uppercases USCounty.
		buf.WriteString(adif.EncodeField("US_COUNTY", strings.ToUpper(station.USCounty)))
	}
	buf.WriteString("\n<EOR>\n\n")

	// ── Section 4: tCONTACT records ──────────────────────────────────────────
	for _, qso := range qsos {
		rec, err := buildContactRecord(privateKey, station, qso)
		if err != nil {
			return nil, fmt.Errorf("sign QSO with %s: %w", qso.Call, err)
		}
		buf.WriteString(rec)
	}

	// ── Gzip compress ────────────────────────────────────────────────────────
	var gz bytes.Buffer
	w, err := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return nil, fmt.Errorf("gzip write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return gz.Bytes(), nil
}

// buildContactRecord builds a single tCONTACT ADIF record including SIGNDATA and
// SIGN_LOTW_V2.0 fields.
func buildContactRecord(key *rsa.PrivateKey, station StationInfo, qso QSO) (string, error) {
	// Normalise time: ensure QSO_DATE is YYYY-MM-DD and QSO_TIME ends with Z.
	qsoDate := normaliseDate(qso.QSODate)
	qsoTime := normaliseTime(qso.QSOTime)

	mode, submode := canonicalADIFModePair(qso.Mode, qso.Submode)
	qso.Mode = mode
	qso.Submode = submode

	// Build the signdata string: station fields + contact fields in sigspec order.
	signdata := buildSigndata(station, qso, qsoDate, qsoTime)

	// Sign with RSA-SHA1 (PKCS1v15) — required by ARRL LoTW.
	//nolint:gosec // SHA-1 is mandated by the LoTW spec
	h := sha1.New()
	h.Write([]byte(signdata))
	digest := h.Sum(nil)

	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, digest)
	if err != nil {
		return "", fmt.Errorf("rsa sign: %w", err)
	}
	sigB64 := base64.StdEncoding.EncodeToString(sigBytes)

	var sb strings.Builder
	sb.WriteString("<Rec_Type:8>tCONTACT\n")
	sb.WriteString("<STATION_UID:1>1\n")
	sb.WriteString(adif.EncodeField("CALL", strings.ToUpper(qso.Call)))
	sb.WriteString(adif.EncodeField("BAND", strings.ToUpper(qso.Band)))
	if qso.BandRX != "" {
		sb.WriteString(adif.EncodeField("BAND_RX", strings.ToUpper(qso.BandRX)))
	}
	sb.WriteString(adif.EncodeField("MODE", strings.ToUpper(qso.Mode)))
	if qso.Submode != "" {
		sb.WriteString(adif.EncodeField("SUBMODE", strings.ToUpper(qso.Submode)))
	}
	if qso.Freq != "" {
		sb.WriteString(adif.EncodeField("FREQ", qso.Freq))
	}
	if qso.FreqRX != "" {
		sb.WriteString(adif.EncodeField("FREQ_RX", qso.FreqRX))
	}
	if qso.PropMode != "" {
		sb.WriteString(adif.EncodeField("PROP_MODE", strings.ToUpper(qso.PropMode)))
	}
	if qso.SatName != "" {
		sb.WriteString(adif.EncodeField("SAT_NAME", strings.ToUpper(qso.SatName)))
	}
	sb.WriteString(adif.EncodeField("QSO_DATE", qsoDate))
	sb.WriteString(adif.EncodeField("QSO_TIME", qsoTime))

	// SIGN_LOTW_V2.0 uses ":6" as the ADIF type specifier (per CloudLog reference).
	// Length is len(sigB64)+1 to account for the trailing newline in the value field.
	sb.WriteString(fmt.Sprintf("<SIGN_LOTW_V2.0:%d:6>%s\n", len(sigB64)+1, sigB64))

	// SIGNDATA contains the raw (unhashed) string that was signed.
	sb.WriteString(adif.EncodeField("SIGNDATA", signdata))

	sb.WriteString("\n<EOR>\n\n")
	return sb.String(), nil
}

// buildSigndata constructs the signdata string in TQSL sigspec order.
//
// Order (from CloudLog adif_export.php, which matches TQSL config.xml sigspecs):
//
//	Station: CA_PROVINCE, CQZ, GRIDSQUARE, IOTA, ITUZ, US_COUNTY, US_STATE
//	Contact: BAND, BAND_RX, CALL, FREQ, FREQ_RX, MODE, PROP_MODE, QSO_DATE, QSO_TIME, SAT_NAME
func canonicalADIFModePair(mode, submode string) (string, string) {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	submode = strings.ToUpper(strings.TrimSpace(submode))

	if submode != "" {
		switch {
		case mode == "MFSK" && submode == "FT8":
			return "FT8", ""
		case mode == "MFSK" && (submode == "FT2" || submode == "FT4" || submode == "JS8" || submode == "Q65"):
			return "MFSK", submode
		case mode == "DIGITALVOICE" && submode == "DMR":
			return "DIGITALVOICE", submode
		default:
			return mode, submode
		}
	}

	switch mode {
	case "FT2", "FT4", "JS8", "Q65":
		return "MFSK", mode
	case "DMR":
		return "DIGITALVOICE", mode
	default:
		return mode, ""
	}
}

func buildSigndata(station StationInfo, qso QSO, qsoDate, qsoTime string) string {
	var sb strings.Builder
	country := strings.ToUpper(station.Country)

	// Station fields first
	if station.CAProvince != "" && country == "CANADA" {
		sb.WriteString(strings.ToUpper(station.CAProvince))
	}
	if station.CQZ != "" {
		sb.WriteString(station.CQZ)
	}
	if station.Gridsquare != "" {
		sb.WriteString(strings.ToUpper(station.Gridsquare))
	}
	if station.IOTA != "" {
		sb.WriteString(strings.ToUpper(station.IOTA))
	}
	if station.ITUZ != "" {
		sb.WriteString(station.ITUZ)
	}
	isUSA := country == "UNITED STATES OF AMERICA" || country == "USA" || country == "ALASKA" || country == "HAWAII"
	if station.USCounty != "" && isUSA {
		sb.WriteString(strings.ToUpper(station.USCounty))
	}
	if station.USState != "" && isUSA {
		sb.WriteString(strings.ToUpper(station.USState))
	}

	// Contact fields
	if qso.Band != "" {
		sb.WriteString(strings.ToUpper(qso.Band))
	}
	if qso.BandRX != "" {
		sb.WriteString(strings.ToUpper(qso.BandRX))
	}
	if qso.Call != "" {
		sb.WriteString(strings.ToUpper(qso.Call))
	}
	if qso.Freq != "" {
		sb.WriteString(qso.Freq)
	}
	if qso.FreqRX != "" {
		sb.WriteString(qso.FreqRX)
	}
	if qso.Mode != "" {
		sb.WriteString(strings.ToUpper(qso.Mode))
	}
	if qso.PropMode != "" {
		sb.WriteString(strings.ToUpper(qso.PropMode))
	}
	sb.WriteString(qsoDate)
	sb.WriteString(qsoTime)
	if qso.SatName != "" {
		sb.WriteString(strings.ToUpper(qso.SatName))
	}

	return sb.String()
}

// normaliseDate converts common date formats to YYYY-MM-DD.
func normaliseDate(d string) string {
	// Try YYYYMMDD
	if len(d) == 8 && !strings.Contains(d, "-") {
		return d[:4] + "-" + d[4:6] + "-" + d[6:8]
	}
	// Try parsing with time package
	for _, layout := range []string{"2006-01-02", "20060102", "2006/01/02"} {
		if t, err := time.Parse(layout, d); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return d
}

// normaliseTime ensures the time string ends with Z (UTC indicator).
func normaliseTime(t string) string {
	t = strings.TrimSpace(t)
	// HHMMSS → HH:MM:SS
	if len(t) == 6 && !strings.Contains(t, ":") {
		t = t[:2] + ":" + t[2:4] + ":" + t[4:6]
	}
	// HHMM → HH:MM:00
	if len(t) == 4 && !strings.Contains(t, ":") {
		t = t[:2] + ":" + t[2:4] + ":00"
	}
	if !strings.HasSuffix(t, "Z") {
		t += "Z"
	}
	return t
}

// zeroKey attempts to zero all private RSA key material from memory.
// It wipes D, Primes, all CRTValues, and the Precomputed Dp/Dq/Qinv values.
func zeroKey(key *rsa.PrivateKey) {
	if key == nil {
		return
	}
	if key.D != nil {
		key.D.SetInt64(0)
	}
	for _, p := range key.Primes {
		if p != nil {
			p.SetInt64(0)
		}
	}
	// Wipe CRT precomputed values — all of them.
	if key.Precomputed.Dp != nil {
		key.Precomputed.Dp.SetInt64(0)
	}
	if key.Precomputed.Dq != nil {
		key.Precomputed.Dq.SetInt64(0)
	}
	if key.Precomputed.Qinv != nil {
		key.Precomputed.Qinv.SetInt64(0)
	}
	for _, pp := range key.Precomputed.CRTValues {
		if pp.Exp != nil {
			pp.Exp.SetInt64(0)
		}
		if pp.Coeff != nil {
			pp.Coeff.SetInt64(0)
		}
		if pp.R != nil {
			pp.R.SetInt64(0)
		}
	}
}

// ZeroKey is the exported form of zeroKey for use by other packages (e.g. server).
func ZeroKey(key *rsa.PrivateKey) { zeroKey(key) }

// ExtractCallsign extracts the amateur callsign from an ARRL-issued X.509 certificate.
// ARRL encodes the callsign as an RDN attribute in the Subject using OID 1.3.6.1.4.1.12348.1.1.
//
// The CommonName fallback is intentionally NOT used: a cert without the ARRL callsign OID
// is not a valid ARRL LoTW certificate, regardless of what its CommonName says.
func ExtractCallsign(cert *x509.Certificate) (string, error) {
	for _, name := range cert.Subject.Names {
		if name.Type.String() == "1.3.6.1.4.1.12348.1.1" {
			if s, ok := name.Value.(string); ok && s != "" {
				return strings.ToUpper(s), nil
			}
		}
	}
	return "", fmt.Errorf("ARRL callsign OID 1.3.6.1.4.1.12348.1.1 not found in certificate Subject — not a valid ARRL LoTW certificate")
}

// ExtractARRLExtensions extracts LoTW-specific certificate extensions.
//
//	OID 1.3.6.1.4.1.12348.1.2 → QSO start date
//	OID 1.3.6.1.4.1.12348.1.3 → QSO end date
//	OID 1.3.6.1.4.1.12348.1.4 → DXCC entity ID
func ExtractARRLExtensions(cert *x509.Certificate) (qsoStart, qsoEnd, dxcc string) {
	for _, ext := range cert.Extensions {
		oidStr := ext.Id.String()
		// Strip ASN.1 tag byte (04 = OCTET STRING) and length for simple strings
		val := asn1StringValue(ext.Value)
		switch oidStr {
		case "1.3.6.1.4.1.12348.1.2":
			qsoStart = val
		case "1.3.6.1.4.1.12348.1.3":
			qsoEnd = val
		case "1.3.6.1.4.1.12348.1.4":
			dxcc = val
		}
	}
	return
}

// asn1StringValue strips basic ASN.1 encoding to get a printable string value.
func asn1StringValue(b []byte) string {
	if len(b) < 2 {
		return string(b)
	}
	// If first byte is a known string tag (0x13=PrintableString, 0x0c=UTF8String,
	// 0x16=IA5String, 0x04=OctetString), skip tag+length.
	switch b[0] {
	case 0x04, 0x13, 0x0c, 0x16, 0x1a:
		length := int(b[1])
		if len(b) >= 2+length {
			return string(b[2 : 2+length])
		}
	}
	return string(b)
}
