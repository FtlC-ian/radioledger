package signer_test

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/signer"
)

// generateTestCert creates a self-signed RSA certificate that mimics an ARRL callsign cert.
// The callsign is embedded in OID 1.3.6.1.4.1.12348.1.1 of the subject.
func generateTestCert(t *testing.T, callsign string) (*rsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// ARRL callsign OID
	callsignOID := asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 12348, 1, 1}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: callsign,
			ExtraNames: []pkix.AttributeTypeAndValue{
				{Type: callsignOID, Value: callsign},
			},
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(3 * 365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	return key, cert, certDER
}

func TestBuildTQ8_BasicStructure(t *testing.T) {
	key, _, certDER := generateTestCert(t, "W1TEST")

	station := signer.StationInfo{
		Callsign:   "W1TEST",
		DXCC:       "291",
		Gridsquare: "FN42",
		Country:    "UNITED STATES OF AMERICA",
	}

	qsos := []signer.QSO{
		{
			Call:    "DL5XYZ",
			Band:    "20m",
			Mode:    "SSB",
			QSODate: "2024-06-15",
			QSOTime: "14:30:00",
		},
	}

	tq8Data, err := signer.BuildTQ8(certDER, key, station, qsos)
	if err != nil {
		t.Fatalf("BuildTQ8: %v", err)
	}

	// Decompress and inspect
	gz, err := gzip.NewReader(bytes.NewReader(tq8Data))
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("decompress tq8: %v", err)
	}

	content := string(raw)

	// Check required sections
	for _, want := range []string{
		"<TQSL_IDENT:",
		"<Rec_Type:5>tCERT",
		"<CERTIFICATE:",
		"<Rec_Type:8>tSTATION",
		"<CALL:6>W1TEST",
		"<DXCC:3>291",
		"<GRIDSQUARE:4>FN42",
		"<Rec_Type:8>tCONTACT",
		"<CALL:6>DL5XYZ",
		"<BAND:3>20M",
		"<MODE:3>SSB",
		"<SIGN_LOTW_V2.0:",
		"<SIGNDATA:",
		"<EOR>",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("tq8 missing expected content: %q", want)
		}
	}
}

func TestBuildTQ8_MultipleQSOs(t *testing.T) {
	key, _, certDER := generateTestCert(t, "K9MULTI")

	station := signer.StationInfo{
		Callsign: "K9MULTI",
		DXCC:     "291",
	}

	qsos := []signer.QSO{
		{Call: "JA1AAA", Band: "40m", Mode: "CW", QSODate: "2024-01-01", QSOTime: "000000"},
		{Call: "EA4BBB", Band: "15m", Mode: "SSB", QSODate: "2024-01-02", QSOTime: "120000"},
		{Call: "VK2CCC", Band: "10m", Mode: "FT8", QSODate: "2024-01-03", QSOTime: "180000"},
	}

	tq8Data, err := signer.BuildTQ8(certDER, key, station, qsos)
	if err != nil {
		t.Fatalf("BuildTQ8: %v", err)
	}

	gz, _ := gzip.NewReader(bytes.NewReader(tq8Data))
	raw, _ := io.ReadAll(gz)
	content := string(raw)

	for _, call := range []string{"JA1AAA", "EA4BBB", "VK2CCC"} {
		if !strings.Contains(content, call) {
			t.Errorf("expected call %q in tq8 output", call)
		}
	}

	// Count tCONTACT sections
	count := strings.Count(content, "<Rec_Type:8>tCONTACT")
	if count != 3 {
		t.Errorf("expected 3 tCONTACT records, got %d", count)
	}
}

func TestBuildTQ8_CanonicalizesAdifModePairs(t *testing.T) {
	key, _, certDER := generateTestCert(t, "W1MODE")
	station := signer.StationInfo{Callsign: "W1MODE", DXCC: "291"}

	qsos := []signer.QSO{{
		Call:    "K1DIGI",
		Band:    "20m",
		Mode:    "FT2",
		QSODate: "2024-06-15",
		QSOTime: "14:30:00",
	}}

	tq8Data, err := signer.BuildTQ8(certDER, key, station, qsos)
	if err != nil {
		t.Fatalf("BuildTQ8: %v", err)
	}

	gz, _ := gzip.NewReader(bytes.NewReader(tq8Data))
	raw, _ := io.ReadAll(gz)
	content := string(raw)

	if !strings.Contains(content, "<MODE:4>MFSK") {
		t.Fatalf("expected canonical MODE=MFSK in tq8 output:\n%s", content)
	}
	if !strings.Contains(content, "<SUBMODE:3>FT2") {
		t.Fatalf("expected canonical SUBMODE=FT2 in tq8 output:\n%s", content)
	}
	if strings.Contains(content, "<MODE:3>FT2") {
		t.Fatalf("unexpected legacy MODE=FT2 in tq8 output:\n%s", content)
	}
}

func TestBuildTQ8_NoQSOs(t *testing.T) {
	key, _, certDER := generateTestCert(t, "W1EMPTY")
	station := signer.StationInfo{Callsign: "W1EMPTY", DXCC: "291"}

	_, err := signer.BuildTQ8(certDER, key, station, nil)
	if err == nil {
		t.Fatal("expected error for empty QSO list")
	}
}

func TestBuildTQ8_SatelliteQSO(t *testing.T) {
	key, _, certDER := generateTestCert(t, "W1SAT")
	station := signer.StationInfo{Callsign: "W1SAT", DXCC: "291"}

	qsos := []signer.QSO{
		{
			Call:     "VE3SAT",
			Band:     "2m",
			BandRX:   "70cm",
			Mode:     "SSB",
			PropMode: "SAT",
			SatName:  "AO-91",
			QSODate:  "2024-03-15",
			QSOTime:  "12:00:00",
		},
	}

	tq8Data, err := signer.BuildTQ8(certDER, key, station, qsos)
	if err != nil {
		t.Fatalf("BuildTQ8: %v", err)
	}

	gz, _ := gzip.NewReader(bytes.NewReader(tq8Data))
	raw, _ := io.ReadAll(gz)
	content := string(raw)

	if !strings.Contains(content, "AO-91") {
		t.Error("expected SAT_NAME in tq8 output")
	}
	if !strings.Contains(content, "PROP_MODE") {
		t.Error("expected PROP_MODE in tq8 output")
	}
}

func TestBuildTQ8_SigndataContainsExpectedFields(t *testing.T) {
	key, _, certDER := generateTestCert(t, "W1SIGN")
	station := signer.StationInfo{
		Callsign:   "W1SIGN",
		DXCC:       "291",
		Gridsquare: "EM72",
		Country:    "UNITED STATES OF AMERICA",
		USState:    "TN",
		CQZ:        "4",
	}

	qsos := []signer.QSO{
		{
			Call:    "G4TEST",
			Band:    "20m",
			Mode:    "FT8",
			Freq:    "14.074",
			QSODate: "2024-08-20",
			QSOTime: "09:15:00",
		},
	}

	tq8Data, err := signer.BuildTQ8(certDER, key, station, qsos)
	if err != nil {
		t.Fatalf("BuildTQ8: %v", err)
	}

	gz, _ := gzip.NewReader(bytes.NewReader(tq8Data))
	raw, _ := io.ReadAll(gz)
	content := string(raw)

	// Find the SIGNDATA field value and check it contains expected substrings.
	// SIGNDATA in ADIF: <SIGNDATA:N>value
	idx := strings.Index(content, "<SIGNDATA:")
	if idx < 0 {
		t.Fatal("SIGNDATA field not found in tq8 output")
	}
	// Parse the length
	rest := content[idx+len("<SIGNDATA:"):]
	endBracket := strings.Index(rest, ">")
	if endBracket < 0 {
		t.Fatal("malformed SIGNDATA tag")
	}
	var length int
	if _, err := io.ReadFull(strings.NewReader(rest[:endBracket]), make([]byte, 0)); err == nil {
		// Use strings.Reader for parsing
		n := 0
		for _, c := range rest[:endBracket] {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		length = n
	}
	sigdata := rest[endBracket+1 : endBracket+1+length]

	// sigdata should include: gridsquare + CQZ + US_STATE + band + callsign + mode + date + time
	for _, fragment := range []string{"EM72", "4", "TN", "20M", "G4TEST", "FT8", "2024-08-20"} {
		if !strings.Contains(sigdata, fragment) {
			t.Errorf("SIGNDATA missing expected fragment %q (full sigdata: %q)", fragment, sigdata)
		}
	}
}

func TestExtractCallsign_ARRLCert(t *testing.T) {
	_, cert, _ := generateTestCert(t, "N0CALL")
	callsign, err := signer.ExtractCallsign(cert)
	if err != nil {
		t.Fatalf("ExtractCallsign: %v", err)
	}
	if callsign != "N0CALL" {
		t.Errorf("callsign = %q, want %q", callsign, "N0CALL")
	}
}

func TestNormaliseDate(t *testing.T) {
	// normaliseDate is internal but we test it indirectly via tq8 output.
	key, _, certDER := generateTestCert(t, "W1DATE")
	station := signer.StationInfo{Callsign: "W1DATE", DXCC: "291"}

	// Test YYYYMMDD format (no dashes)
	qsos := []signer.QSO{{Call: "VE1X", Band: "20m", Mode: "CW", QSODate: "20240615", QSOTime: "143000"}}
	tq8Data, err := signer.BuildTQ8(certDER, key, station, qsos)
	if err != nil {
		t.Fatalf("BuildTQ8: %v", err)
	}
	gz, _ := gzip.NewReader(bytes.NewReader(tq8Data))
	raw, _ := io.ReadAll(gz)
	if !strings.Contains(string(raw), "2024-06-15") {
		t.Error("expected normalised date 2024-06-15 in output")
	}
}
