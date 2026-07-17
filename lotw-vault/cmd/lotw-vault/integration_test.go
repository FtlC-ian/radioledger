package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/certstore"
	"github.com/FtlC-ian/radioledger/lotw-vault/internal/server"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

// arrlCallsignOID is the ARRL OID used to encode the callsign as an RDN in the Subject.
var arrlCallsignOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 12348, 1, 1}

// selfSignedP12 generates a self-signed RSA certificate with the ARRL callsign OID
// properly encoded in Subject.ExtraNames (as an RDN attribute, not a certificate extension)
// and packages it as PKCS#12.
//
// These test certs are intentionally self-signed, so integration tests MUST call
// startTestServer with server.WithSkipChainVerify().
func selfSignedP12(t *testing.T, callsign string) (p12Data []byte, p12Password string) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   callsign,
			Organization: []string{"ARRL"},
			// ARRL callsign OID 1.3.6.1.4.1.12348.1.1 as an RDN Subject attribute.
			// This is what ExtractCallsign looks for.
			ExtraNames: []pkix.AttributeTypeAndValue{
				{
					Type:  arrlCallsignOID,
					Value: callsign,
				},
			},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}

	p12Password = "testpass"
	p12Data, err = pkcs12.Modern2023.Encode(privKey, cert, nil, p12Password)
	if err != nil {
		t.Fatalf("encode p12: %v", err)
	}
	return p12Data, p12Password
}

// startTestServer launches the vault server on a random port and returns
// the base URL and a cancel func to shut it down.
func startTestServer(t *testing.T) (baseURL string, cancel context.CancelFunc) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := certstore.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}

	// Use WithSkipChainVerify because test certs are self-signed, not ARRL-issued.
	srv := server.New(store, server.WithSkipChainVerify())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	httpSrv := &http.Server{
		Handler:      srv.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = httpSrv.Serve(ln)
	}()
	t.Cleanup(func() {
		cancel()
		_ = httpSrv.Shutdown(context.Background())
		store.Close()
	})
	_ = ctx

	baseURL = "http://" + ln.Addr().String()
	return baseURL, cancel
}

// postMultipart is a helper to POST multipart form data.
func postMultipart(t *testing.T, url string, fields map[string]string, fileField, fileName string, fileData []byte) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}

	if fileField != "" {
		fw, err := mw.CreateFormFile(fileField, fileName)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := fw.Write(fileData); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	}
	mw.Close()

	resp, err := http.Post(url, mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// postJSON is a helper to POST JSON body.
func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func mustReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func assertStatus(t *testing.T, resp *http.Response, want int) string {
	t.Helper()
	body := mustReadBody(t, resp)
	if resp.StatusCode != want {
		t.Errorf("want HTTP %d, got %d; body: %s", want, resp.StatusCode, body)
	}
	return body
}

// ── sampleADIF returns a minimal valid ADIF string with one QSO ──────────────

func sampleADIF() string {
	return strings.Join([]string{
		"<CALL:4>W1AW",
		"<BAND:3>40M",
		"<MODE:2>CW",
		"<QSO_DATE:8>20240101",
		"<TIME_ON:4>1200",
		"<EOR>",
	}, " ")
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	base, _ := startTestServer(t)

	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	assertStatus(t, resp, http.StatusOK)
}

func TestFullFlow(t *testing.T) {
	base, _ := startTestServer(t)
	const userID = "testuser1"
	const userPass = "hunter2"

	p12Data, p12Pass := selfSignedP12(t, "W1TEST")

	// ── 1. Import cert ─────────────────────────────────────────────────────────
	t.Run("import cert", func(t *testing.T) {
		resp := postMultipart(t, base+"/import-cert",
			map[string]string{
				"user_id":       userID,
				"p12_password":  p12Pass,
				"user_password": userPass,
			},
			"p12_file", "cert.p12", p12Data,
		)
		body := assertStatus(t, resp, http.StatusOK)
		if !strings.Contains(body, "W1TEST") {
			t.Errorf("expected callsign W1TEST in response, got: %s", body)
		}
	})

	// ── 2. Cert info (no password required) ───────────────────────────────────
	t.Run("cert-info", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("%s/cert-info?user_id=%s", base, userID))
		if err != nil {
			t.Fatalf("GET /cert-info: %v", err)
		}
		body := assertStatus(t, resp, http.StatusOK)
		if !strings.Contains(body, "W1TEST") {
			t.Errorf("expected callsign in cert-info, got: %s", body)
		}
		if !strings.Contains(body, `"expired":false`) {
			t.Errorf("expected expired=false in cert-info, got: %s", body)
		}
	})

	// ── 3. Sign ADIF ───────────────────────────────────────────────────────────
	var tq8Data []byte
	t.Run("sign ADIF", func(t *testing.T) {
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       userID,
			"user_password": userPass,
			"adif_data":     sampleADIF(),
		})
		if resp.StatusCode != http.StatusOK {
			body := mustReadBody(t, resp)
			t.Fatalf("want HTTP 200, got %d; body: %s", resp.StatusCode, body)
		}
		defer resp.Body.Close()
		var err error
		tq8Data, err = io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read tq8: %v", err)
		}
		if len(tq8Data) == 0 {
			t.Fatal("empty tq8 response")
		}
		// Verify it's valid gzip.
		gr, err := gzip.NewReader(bytes.NewReader(tq8Data))
		if err != nil {
			t.Fatalf("tq8 is not valid gzip: %v", err)
		}
		content, err := io.ReadAll(gr)
		if err != nil {
			t.Fatalf("decompress tq8: %v", err)
		}
		contentStr := string(content)
		if !strings.Contains(contentStr, "tCONTACT") {
			t.Errorf("tq8 content missing tCONTACT section; got prefix: %.200s", contentStr)
		}
		if !strings.Contains(contentStr, "SIGN_LOTW_V2.0") {
			t.Errorf("tq8 content missing SIGN_LOTW_V2.0; got prefix: %.200s", contentStr)
		}
	})

	// ── 4. Rotate password ─────────────────────────────────────────────────────
	const newPass = "s3cr3t_new_pass"
	t.Run("rotate password", func(t *testing.T) {
		resp := postJSON(t, base+"/rotate-password", map[string]string{
			"user_id":      userID,
			"old_password": userPass,
			"new_password": newPass,
		})
		body := assertStatus(t, resp, http.StatusOK)
		if !strings.Contains(body, "password rotated") {
			t.Errorf("unexpected response: %s", body)
		}
	})

	// ── 5. Sign again with new password ───────────────────────────────────────
	t.Run("sign with new password", func(t *testing.T) {
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       userID,
			"user_password": newPass,
			"adif_data":     sampleADIF(),
		})
		if resp.StatusCode != http.StatusOK {
			body := mustReadBody(t, resp)
			t.Fatalf("sign after rotate: want HTTP 200, got %d; body: %s", resp.StatusCode, body)
		}
		defer resp.Body.Close()
		newTQ8, _ := io.ReadAll(resp.Body)
		if len(newTQ8) == 0 {
			t.Fatal("empty tq8 after password rotation")
		}
	})

	// ── 6. Sign with old password must fail after rotation ────────────────────
	t.Run("old password rejected after rotate", func(t *testing.T) {
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       userID,
			"user_password": userPass, // old password
			"adif_data":     sampleADIF(),
		})
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	// ── 7. Delete cert ────────────────────────────────────────────────────────
	t.Run("delete cert", func(t *testing.T) {
		// Must use new password.
		req, _ := http.NewRequest(http.MethodDelete, base+"/cert", nil)
		data, _ := json.Marshal(map[string]string{
			"user_id":       userID,
			"user_password": newPass,
		})
		req.Body = io.NopCloser(bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("DELETE /cert: %v", err)
		}
		assertStatus(t, resp, http.StatusOK)
	})

	// ── 8. Verify cert is gone ─────────────────────────────────────────────────
	t.Run("cert gone after delete", func(t *testing.T) {
		resp, _ := http.Get(fmt.Sprintf("%s/cert-info?user_id=%s", base, userID))
		assertStatus(t, resp, http.StatusNotFound)
	})
}

func TestErrorCases(t *testing.T) {
	base, _ := startTestServer(t)
	const userID = "erruser"
	const userPass = "testpassword"

	p12Data, p12Pass := selfSignedP12(t, "W2ERR")

	// Import a cert first so we can test wrong-password cases.
	resp := postMultipart(t, base+"/import-cert",
		map[string]string{
			"user_id":       userID,
			"p12_password":  p12Pass,
			"user_password": userPass,
		},
		"p12_file", "cert.p12", p12Data,
	)
	assertStatus(t, resp, http.StatusOK)

	t.Run("wrong password on sign", func(t *testing.T) {
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       userID,
			"user_password": "wrongpassword",
			"adif_data":     sampleADIF(),
		})
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("missing cert", func(t *testing.T) {
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       "does_not_exist",
			"user_password": userPass,
			"adif_data":     sampleADIF(),
		})
		assertStatus(t, resp, http.StatusNotFound)
	})

	t.Run("invalid ADIF (empty)", func(t *testing.T) {
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       userID,
			"user_password": userPass,
			"adif_data":     "this is not adif at all",
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("invalid ADIF (incomplete records)", func(t *testing.T) {
		// Missing required fields (no BAND, MODE, etc.)
		resp := postJSON(t, base+"/sign", map[string]any{
			"user_id":       userID,
			"user_password": userPass,
			"adif_data":     "<CALL:4>W1AW <EOR>",
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("cert-info missing user_id", func(t *testing.T) {
		resp, _ := http.Get(base + "/cert-info")
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("cert-info unknown user", func(t *testing.T) {
		resp, _ := http.Get(base + "/cert-info?user_id=nobody")
		assertStatus(t, resp, http.StatusNotFound)
	})

	t.Run("rotate with wrong old password", func(t *testing.T) {
		resp := postJSON(t, base+"/rotate-password", map[string]string{
			"user_id":      userID,
			"old_password": "wrongpassword",
			"new_password": "newpassword123",
		})
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("rotate same password rejected", func(t *testing.T) {
		resp := postJSON(t, base+"/rotate-password", map[string]string{
			"user_id":      userID,
			"old_password": userPass,
			"new_password": userPass,
		})
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("import cert with wrong p12 password", func(t *testing.T) {
		resp := postMultipart(t, base+"/import-cert",
			map[string]string{
				"user_id":       "newuser",
				"p12_password":  "wrongp12pass",
				"user_password": "somepass",
			},
			"p12_file", "cert.p12", p12Data,
		)
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("upload endpoint removed", func(t *testing.T) {
		resp, err := http.Post(base+"/upload", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("POST /upload: %v", err)
		}
		defer resp.Body.Close()
		// Go's default mux returns 405 for method not found or 404 for unknown path.
		// Either way it must NOT be 200.
		if resp.StatusCode == http.StatusOK {
			t.Error("/upload should not exist (404 or 405 expected)")
		}
	})
}

// TestSQLiteStorePersistence verifies that data survives a store close/reopen.
func TestSQLiteStorePersistence(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	writeStore, err := certstore.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	entry := &certstore.EncryptedEntry{
		Meta: certstore.CertMeta{
			UserID:    "persistuser",
			Callsign:  "W3PERSIST",
			DXCC:      "291",
			NotBefore: time.Now().Add(-time.Hour),
			NotAfter:  time.Now().Add(365 * 24 * time.Hour),
			CertDER:   makeFakeCertDER(t, "W3PERSIST"),
		},
		EncryptedKey:  []byte("fakeciphertext"),
		Argon2Salt:    []byte("fakesalt12345678"),
		Argon2Time:    2,
		Argon2Memory:  64 * 1024,
		Argon2Threads: 4,
	}

	if err := writeStore.Save(entry); err != nil {
		t.Fatalf("save: %v", err)
	}
	writeStore.Close()

	// Reopen and verify data is there.
	readStore, err := certstore.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer readStore.Close()

	loaded, err := readStore.Load("persistuser")
	if err != nil {
		t.Fatalf("load after reopen: %v", err)
	}
	if loaded.Meta.Callsign != "W3PERSIST" {
		t.Errorf("callsign mismatch: want W3PERSIST, got %s", loaded.Meta.Callsign)
	}
	if !readStore.Exists("persistuser") {
		t.Error("Exists returned false after reopen")
	}

	if err := readStore.Delete("persistuser"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if readStore.Exists("persistuser") {
		t.Error("Exists returned true after delete")
	}
}

// makeFakeCertDER creates a minimal self-signed cert DER for store tests.
func makeFakeCertDER(t *testing.T, cn string) []byte {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 1024) // small key for test speed
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return der
}

// Ensure the binary still compiles — force import of os to avoid "imported
// and not used" if the compiler somehow prunes it.
var _ = os.DevNull
