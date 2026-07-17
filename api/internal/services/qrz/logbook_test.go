package qrz

// Unit tests for the QRZ Logbook API client.
// All tests use an httptest.Server — no real network calls are made.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test helpers
// ──────────────────────────────────────────────────────────────────────────────

// newMockLogbookServer builds an httptest.Server that returns fixed responses
// keyed by the ACTION parameter in the POST body.
func newMockLogbookServer(t *testing.T, handler func(action, adif string, w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		action := r.FormValue("ACTION")
		adif := r.FormValue("ADIF")
		w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
		handler(action, adif, w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// logbookClientFor returns a LogbookClient pointed at the mock server.
func logbookClientFor(t *testing.T, srv *httptest.Server) *LogbookClient {
	t.Helper()
	return NewLogbookClient("TEST-KEY-ABCD").withEndpointRewriter(srv.URL)
}

// ──────────────────────────────────────────────────────────────────────────────
// Credential helpers
// ──────────────────────────────────────────────────────────────────────────────

func TestEncodeDecodeLogbookCredentials(t *testing.T) {
	raw, err := EncodeLogbookCredentials("MY-API-KEY-1234")
	if err != nil {
		t.Fatalf("EncodeLogbookCredentials: %v", err)
	}

	creds, err := DecodeLogbookCredentials(raw)
	if err != nil {
		t.Fatalf("DecodeLogbookCredentials: %v", err)
	}
	if creds.APIKey != "MY-API-KEY-1234" {
		t.Errorf("expected API key MY-API-KEY-1234, got %q", creds.APIKey)
	}
}

func TestEncodeLogbookCredentials_RejectsEmpty(t *testing.T) {
	_, err := EncodeLogbookCredentials("")
	if err == nil {
		t.Error("expected error for empty api_key, got nil")
	}
}

func TestDecodeLogbookCredentials_RejectsMissingKey(t *testing.T) {
	_, err := DecodeLogbookCredentials([]byte(`{"api_key":""}`))
	if err == nil {
		t.Error("expected error for empty api_key in JSON, got nil")
	}
}

func TestIsLogbookCredential(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
		want      bool
	}{
		{"json with api_key", `{"api_key":"XXXX-XXXX"}`, true},
		{"username:password", "W5ABC:mypassword", false},
		{"json without api_key", `{"username":"x","password":"y"}`, false},
		{"empty", "", false},
		{"garbage", "notjson", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsLogbookCredential([]byte(tc.plaintext))
			if got != tc.want {
				t.Errorf("IsLogbookCredential(%q) = %v, want %v", tc.plaintext, got, tc.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// VerifyKey
// ──────────────────────────────────────────────────────────────────────────────

func TestLogbookClient_VerifyKey_OK(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=OK&DATA=TOTAL%3D50%26CONFIRMED%3D20")
	})
	client := logbookClientFor(t, srv)

	if err := client.VerifyKey(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogbookClient_VerifyKey_InvalidKey(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=AUTH&REASON=Invalid+API+Key")
	})
	client := logbookClientFor(t, srv)

	err := client.VerifyKey(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid API key, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected 'invalid' in error, got %v", err)
	}
}

func TestLogbookClient_VerifyKey_Fail(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=FAIL&REASON=Service+Unavailable")
	})
	client := logbookClientFor(t, srv)

	err := client.VerifyKey(context.Background())
	if err == nil {
		t.Fatal("expected error for FAIL result, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// InsertQSO
// ──────────────────────────────────────────────────────────────────────────────

func TestLogbookClient_InsertQSO_Success(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, adif string, w http.ResponseWriter) {
		if action != "INSERT" {
			t.Errorf("expected ACTION=INSERT, got %s", action)
		}
		_, _ = fmt.Fprint(w, "RESULT=OK&LOGID=130877825&COUNT=1")
	})
	client := logbookClientFor(t, srv)

	adif := "<call:4>W5AB<band:3>40m<mode:3>SSB<qso_date:8>20240101<time_on:4>1234<eor>"
	result, err := client.InsertQSO(context.Background(), adif)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.LogID != "130877825" {
		t.Errorf("expected LogID 130877825, got %q", result.LogID)
	}
	if result.Count != 1 {
		t.Errorf("expected Count 1, got %d", result.Count)
	}
	if result.Result != "OK" {
		t.Errorf("expected Result OK, got %q", result.Result)
	}
}

func TestLogbookClient_InsertQSO_Replace(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=REPLACE&LOGID=999&COUNT=1")
	})
	client := logbookClientFor(t, srv)

	result, err := client.InsertQSO(context.Background(), "<call:4>W5AB<eor>")
	if err != nil {
		t.Fatalf("expected REPLACE to succeed, got: %v", err)
	}
	if result.Result != "REPLACE" {
		t.Errorf("expected Result=REPLACE, got %q", result.Result)
	}
	if result.LogID != "999" {
		t.Errorf("expected LogID 999, got %q", result.LogID)
	}
}

func TestLogbookClient_InsertQSO_AuthError(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=AUTH&REASON=Subscription+Required")
	})
	client := logbookClientFor(t, srv)

	_, err := client.InsertQSO(context.Background(), "<call:4>W5AB<eor>")
	if err == nil {
		t.Fatal("expected AUTH error, got nil")
	}
	if !strings.Contains(err.Error(), "subscription") || !strings.Contains(strings.ToLower(err.Error()), "required") {
		t.Errorf("expected subscription error, got: %v", err)
	}
}

func TestLogbookClient_InsertQSO_Fail(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=FAIL&REASON=Invalid+QSO+data")
	})
	client := logbookClientFor(t, srv)

	_, err := client.InsertQSO(context.Background(), "<call:4>W5AB<eor>")
	if err == nil {
		t.Fatal("expected FAIL error, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid QSO data") {
		t.Errorf("expected reason in error, got: %v", err)
	}
}

func TestLogbookClient_InsertQSO_OKWithoutLogID(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		// Malformed response: RESULT=OK but no LOGID.
		_, _ = fmt.Fprint(w, "RESULT=OK&COUNT=1")
	})
	client := logbookClientFor(t, srv)

	_, err := client.InsertQSO(context.Background(), "<call:4>W5AB<eor>")
	if err == nil {
		t.Error("expected error when LOGID is missing, got nil")
	}
}

func TestLogbookClient_ReplaceQSO_UsesReplaceActionAndLogID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.FormValue("ACTION"); got != "REPLACE" {
			t.Fatalf("expected ACTION=REPLACE, got %q", got)
		}
		if got := r.FormValue("LOGID"); got != "777" {
			t.Fatalf("expected LOGID=777, got %q", got)
		}
		if got := r.FormValue("ADIF"); !strings.Contains(got, "<call:4>W5AB") {
			t.Fatalf("expected ADIF payload, got %q", got)
		}
		_, _ = fmt.Fprint(w, "RESULT=OK&LOGID=777&COUNT=1")
	}))
	t.Cleanup(srv.Close)

	client := logbookClientFor(t, srv)
	result, err := client.ReplaceQSO(context.Background(), "777", "<call:4>W5AB<eor>")
	if err != nil {
		t.Fatalf("ReplaceQSO: %v", err)
	}
	if result.LogID != "777" {
		t.Fatalf("expected LogID 777, got %q", result.LogID)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CheckStatus
// ──────────────────────────────────────────────────────────────────────────────

func TestLogbookClient_CheckStatus_OK(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		if action != "STATUS" {
			t.Errorf("expected ACTION=STATUS, got %s", action)
		}
		// DATA contains TOTAL and CONFIRMED separated by & but URL-encoded.
		_, _ = fmt.Fprint(w, "RESULT=OK&DATA=TOTAL%3D150%26CONFIRMED%3D120")
	})
	client := logbookClientFor(t, srv)

	status, err := client.CheckStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Total != 150 {
		t.Errorf("expected Total 150, got %d", status.Total)
	}
	if status.Confirmed != 120 {
		t.Errorf("expected Confirmed 120, got %d", status.Confirmed)
	}
}

func TestLogbookClient_CheckStatus_AuthError(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=AUTH&REASON=Invalid+Key")
	})
	client := logbookClientFor(t, srv)

	_, err := client.CheckStatus(context.Background())
	if err == nil {
		t.Fatal("expected AUTH error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid") {
		t.Errorf("expected 'invalid' in error, got: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CheckLogIDStatus
// ──────────────────────────────────────────────────────────────────────────────

func TestLogbookClient_CheckLogIDStatus_PerQSO(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		// Simulate per-logid confirmation data: 111=Y (confirmed), 222=N (not confirmed).
		_, _ = fmt.Fprint(w, "RESULT=OK&DATA=111%3DY%26222%3DN")
	})
	client := logbookClientFor(t, srv)

	status, err := client.CheckLogIDStatus(context.Background(), []string{"111", "222"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.PerLogID["111"] {
		t.Error("expected logid 111 to be confirmed")
	}
	if status.PerLogID["222"] {
		t.Error("expected logid 222 to be not confirmed")
	}
}

func TestLogbookClient_CheckLogIDStatus_EmptyLogIDs(t *testing.T) {
	// Should short-circuit without an HTTP call.
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		t.Error("should not have made an HTTP call for empty logids")
	})
	client := logbookClientFor(t, srv)

	status, err := client.CheckLogIDStatus(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Result != "OK" {
		t.Errorf("expected Result OK, got %q", status.Result)
	}
}

func TestLogbookClient_FetchConfirmedQSOs_OK(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		if action != "FETCH" {
			t.Errorf("expected ACTION=FETCH, got %s", action)
		}
		_, _ = fmt.Fprint(w, "RESULT=OK&COUNT=1&ADIF=%3CCALL%3A4%3EW1AW%3CBAND%3A3%3E20m%3CMODE%3A2%3ECW%3CQSO_DATE%3A8%3E20240315%3CTIME_ON%3A4%3E1230%3CLOTW_QSL_RCVD%3A1%3EY%3CEOR%3E")
	})
	client := logbookClientFor(t, srv)

	res, err := client.FetchConfirmedQSOs(context.Background(), time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), 250)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("expected count 1, got %d", res.Count)
	}
	if !strings.Contains(res.ADIF, "<CALL:4>W1AW") {
		t.Fatalf("expected ADIF payload, got %q", res.ADIF)
	}
}

func TestLogbookClient_FetchConfirmedQSOs_Auth(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		_, _ = fmt.Fprint(w, "RESULT=AUTH&REASON=Invalid+API+Key")
	})
	client := logbookClientFor(t, srv)

	_, err := client.FetchConfirmedQSOs(context.Background(), time.Time{}, 100)
	if err == nil {
		t.Fatal("expected auth error")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// DeleteQSOs
// ──────────────────────────────────────────────────────────────────────────────

func TestLogbookClient_DeleteQSOs_Success(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		if action != "DELETE" {
			t.Errorf("expected ACTION=DELETE, got %s", action)
		}
		_, _ = fmt.Fprint(w, "RESULT=OK&COUNT=2")
	})
	client := logbookClientFor(t, srv)

	result, err := client.DeleteQSOs(context.Background(), []string{"111", "222"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result != "OK" {
		t.Errorf("expected Result OK, got %q", result.Result)
	}
	if result.Count != 2 {
		t.Errorf("expected Count 2, got %d", result.Count)
	}
}

func TestLogbookClient_DeleteQSOs_Empty(t *testing.T) {
	srv := newMockLogbookServer(t, func(action, _ string, w http.ResponseWriter) {
		t.Error("should not have made an HTTP call for empty logids")
	})
	client := logbookClientFor(t, srv)

	result, err := client.DeleteQSOs(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result != "OK" {
		t.Errorf("expected Result OK, got %q", result.Result)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// parseStatusData
// ──────────────────────────────────────────────────────────────────────────────

func TestParseStatusData_Aggregate(t *testing.T) {
	sr := &StatusResult{PerLogID: map[string]bool{}}
	parseStatusData("TOTAL=200&CONFIRMED=150", sr)
	if sr.Total != 200 {
		t.Errorf("expected Total 200, got %d", sr.Total)
	}
	if sr.Confirmed != 150 {
		t.Errorf("expected Confirmed 150, got %d", sr.Confirmed)
	}
}

func TestParseStatusData_PerLogID(t *testing.T) {
	sr := &StatusResult{PerLogID: map[string]bool{}}
	parseStatusData("130877825=Y&130877826=N", sr)
	if !sr.PerLogID["130877825"] {
		t.Error("expected logid 130877825 confirmed")
	}
	if sr.PerLogID["130877826"] {
		t.Error("expected logid 130877826 not confirmed")
	}
}

func TestParseStatusData_Empty(t *testing.T) {
	sr := &StatusResult{PerLogID: map[string]bool{}}
	parseStatusData("", sr) // Must not panic.
	if sr.Total != 0 || sr.Confirmed != 0 {
		t.Error("expected zero values for empty data")
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"123", true},
		{"0", true},
		{"", false},
		{"abc", false},
		{"12a", false},
		{"130877825", true},
	}
	for _, tc := range tests {
		if isNumeric(tc.s) != tc.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tc.s, !tc.want, tc.want)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// parseQRZResponse (regression test for issue #121)
// ──────────────────────────────────────────────────────────────────────────────

func TestParseQRZResponse_LargeADIF(t *testing.T) {
	// Simulate a FETCH response with 90 QSOs. The ADIF blob contains many '&'
	// characters in tags like <email:12&nbsp;foo> or newlines between records.
	// Prior to fix: url.ParseQuery would treat each '&' as a param separator,
	// exceeding Go's 1000-param limit and failing with "number of URL query
	// parameters exceeded limit".
	//
	// After fix: parseQRZResponse splits at &ADIF= and only parses the prefix.

	adifBlob := strings.Repeat("<station_callsign:5>AF5SH\n<name:17>Test Operator\n<email:20>test&user@example.com\n<eor>\n", 90)
	body := fmt.Sprintf("RESULT=OK&COUNT=90&ADIF=%s", adifBlob)

	vals := parseQRZResponse(body)

	if vals.Get("RESULT") != "OK" {
		t.Errorf("expected RESULT=OK, got %q", vals.Get("RESULT"))
	}
	if vals.Get("COUNT") != "90" {
		t.Errorf("expected COUNT=90, got %q", vals.Get("COUNT"))
	}
	adif := vals.Get("ADIF")
	if !strings.Contains(adif, "AF5SH") {
		t.Errorf("expected ADIF to contain AF5SH, got %q", adif)
	}
	if !strings.Contains(adif, "test&user@example.com") {
		t.Errorf("expected ADIF to preserve '&' in email, got %q", adif)
	}
}

func TestParseQRZResponse_NoADIF(t *testing.T) {
	body := "RESULT=OK&LOGID=123&COUNT=1"
	vals := parseQRZResponse(body)

	if vals.Get("RESULT") != "OK" {
		t.Errorf("expected RESULT=OK, got %q", vals.Get("RESULT"))
	}
	if vals.Get("LOGID") != "123" {
		t.Errorf("expected LOGID=123, got %q", vals.Get("LOGID"))
	}
	if vals.Get("COUNT") != "1" {
		t.Errorf("expected COUNT=1, got %q", vals.Get("COUNT"))
	}
}

func TestParseQRZResponse_ADIFFirst(t *testing.T) {
	// Edge case: response starts with ADIF= (no leading params).
	body := "ADIF=<call:4>W1AW<eor>"
	vals := parseQRZResponse(body)

	if vals.Get("ADIF") != "<call:4>W1AW<eor>" {
		t.Errorf("expected full ADIF, got %q", vals.Get("ADIF"))
	}
}
