package lotw

import (
	"context"
	"testing"
	"time"
)

func TestDecodeStoredPasswords_LegacyVaultOnly(t *testing.T) {
	got, err := DecodeStoredPasswords([]byte("vault-secret"))
	if err != nil {
		t.Fatalf("DecodeStoredPasswords: %v", err)
	}
	if got.VaultPassword != "vault-secret" {
		t.Fatalf("VaultPassword = %q, want %q", got.VaultPassword, "vault-secret")
	}
	if got.WebPassword != "" {
		t.Fatalf("WebPassword = %q, want empty", got.WebPassword)
	}
}

func TestEncodeDecodeStoredPasswords_JSONRoundTrip(t *testing.T) {
	encoded, err := EncodeStoredPasswords(StoredPasswords{VaultPassword: "vault", WebPassword: "web"})
	if err != nil {
		t.Fatalf("EncodeStoredPasswords: %v", err)
	}
	got, err := DecodeStoredPasswords(encoded)
	if err != nil {
		t.Fatalf("DecodeStoredPasswords: %v", err)
	}
	if got.VaultPassword != "vault" || got.WebPassword != "web" {
		t.Fatalf("decoded = %+v, want vault/web", got)
	}
}

func TestParseReportResponse(t *testing.T) {
	adif := `<PROGRAMID:4>LoTW<APP_LoTW_LASTQSL:19>2026-03-15 13:14:15<EOH>
<CALL:5>K1ABC<BAND:3>20m<MODE:3>SSB<QSO_DATE:8>20260314<TIME_ON:6>011530<QSL_RCVD:1>Y<QSLRDATE:8>20260315<EOR>`

	result, err := parseReportResponse(context.Background(), adif)
	if err != nil {
		t.Fatalf("parseReportResponse: %v", err)
	}
	if result.LastQSLAt == nil {
		t.Fatal("LastQSLAt = nil, want value")
	}
	wantLast := time.Date(2026, 3, 15, 13, 14, 15, 0, time.UTC)
	if !result.LastQSLAt.Equal(wantLast) {
		t.Fatalf("LastQSLAt = %v, want %v", result.LastQSLAt, wantLast)
	}
	if len(result.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(result.Records))
	}
	rec := result.Records[0]
	if rec.Callsign != "K1ABC" || rec.Band != "20m" || rec.Mode != "SSB" {
		t.Fatalf("record identity = %+v", rec)
	}
	wantOn := time.Date(2026, 3, 14, 1, 15, 30, 0, time.UTC)
	if !rec.DatetimeOn.Equal(wantOn) {
		t.Fatalf("DatetimeOn = %v, want %v", rec.DatetimeOn, wantOn)
	}
	wantQSL := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	if !rec.QSLDate.Equal(wantQSL) {
		t.Fatalf("QSLDate = %v, want %v", rec.QSLDate, wantQSL)
	}
}
