// Package server implements the HTTP API for the LoTW Signing Vault.
//
// Endpoints:
//
//	POST   /import-cert     – import a .p12 certificate (encrypted key storage)
//	POST   /sign            – sign an ADIF log → returns .tq8 file
//	POST   /rotate-password – re-encrypt stored key under a new password
//	DELETE /cert            – delete a stored certificate
//	GET    /cert-info       – return public cert metadata (no key material)
//	GET    /health          – liveness check
//
// Note: /upload was intentionally removed. The vault has no internet access.
// Upload signed .tq8 files from the main RadioLedger API using internal/lotw/upload.go.
package server

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/adif"
	"github.com/FtlC-ian/radioledger/lotw-vault/internal/certstore"
	vaultcrypto "github.com/FtlC-ian/radioledger/lotw-vault/internal/crypto"
	"github.com/FtlC-ian/radioledger/lotw-vault/internal/signer"
)

const maxUploadSize = 10 << 20 // 10 MiB

// Server holds the vault's dependencies.
type Server struct {
	store           certstore.Store
	mux             *http.ServeMux
	skipChainVerify bool
}

// Option is a functional option for configuring a Server.
type Option func(*Server)

// WithSkipChainVerify disables ARRL certificate chain verification.
// Use ONLY in development/testing with self-signed certificates.
// Production deployments must not set this option.
func WithSkipChainVerify() Option {
	return func(s *Server) {
		s.skipChainVerify = true
	}
}

// New creates a new Server with the given store and optional configuration.
func New(store certstore.Store, opts ...Option) *Server {
	s := &Server{store: store}
	for _, opt := range opts {
		opt(s)
	}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /import-cert", s.handleImportCert)
	mux.HandleFunc("POST /sign", s.handleSign)
	mux.HandleFunc("POST /rotate-password", s.handleRotatePassword)
	mux.HandleFunc("DELETE /cert", s.handleDeleteCert)
	mux.HandleFunc("GET /cert-info", s.handleCertInfo)
	mux.HandleFunc("GET /health", s.handleHealth)

	s.mux = mux
	return s
}

// Handler returns the http.Handler for the server.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// ── GET /health ───────────────────────────────────────────────────────────────

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// ── GET /cert-info ────────────────────────────────────────────────────────────

type certInfoResponse struct {
	UserID     string `json:"user_id"`
	Callsign   string `json:"callsign"`
	DXCC       string `json:"dxcc,omitempty"`
	Gridsquare string `json:"gridsquare,omitempty"`
	CQZ        string `json:"cqz,omitempty"`
	ITUZ       string `json:"ituz,omitempty"`
	QSOStart   string `json:"qso_start,omitempty"`
	QSOEnd     string `json:"qso_end,omitempty"`
	NotBefore  string `json:"cert_not_before"`
	NotAfter   string `json:"cert_not_after"`
	Expired    bool   `json:"expired"`
}

func (s *Server) handleCertInfo(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id query parameter is required")
		return
	}

	entry, err := s.store.Load(userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no certificate found for user")
		return
	}

	writeJSON(w, http.StatusOK, certInfoResponse{
		UserID:     entry.Meta.UserID,
		Callsign:   entry.Meta.Callsign,
		DXCC:       entry.Meta.DXCC,
		Gridsquare: entry.Meta.Gridsquare,
		CQZ:        entry.Meta.CQZ,
		ITUZ:       entry.Meta.ITUZ,
		QSOStart:   entry.Meta.QSOStart,
		QSOEnd:     entry.Meta.QSOEnd,
		NotBefore:  entry.Meta.NotBefore.Format(time.RFC3339),
		NotAfter:   entry.Meta.NotAfter.Format(time.RFC3339),
		Expired:    time.Now().After(entry.Meta.NotAfter),
	})
}

// ── POST /import-cert ─────────────────────────────────────────────────────────

type importResponse struct {
	Callsign  string `json:"callsign"`
	DXCC      string `json:"dxcc"`
	QSOStart  string `json:"qso_start"`
	QSOEnd    string `json:"qso_end"`
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
	Message   string `json:"message"`
}

func (s *Server) handleImportCert(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	// Always clean up multipart temp files, even if the handler returns early.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	userID := strings.TrimSpace(r.FormValue("user_id"))
	p12Password := r.FormValue("p12_password")
	userPassword := r.FormValue("user_password")

	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if userPassword == "" {
		writeError(w, http.StatusBadRequest, "user_password is required")
		return
	}

	file, _, err := r.FormFile("p12_file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "p12_file is required")
		return
	}
	defer file.Close()

	p12Data, err := io.ReadAll(io.LimitReader(file, maxUploadSize))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read certificate file")
		return
	}
	// Zero the raw p12 bytes once we're done with them.
	defer vaultcrypto.ZeroBytes(p12Data)

	// Parse .p12 — extract private key, user certificate, and CA chain.
	privateKey, cert, caChain, err := pkcs12.DecodeChain(p12Data, p12Password)
	if err != nil {
		// Don't echo the parse error — it could leak p12 internals.
		log.Printf("[vault] import-cert p12 decode failed for user %q: %v", userID, err)
		writeError(w, http.StatusBadRequest, "invalid certificate file or password")
		return
	}
	rsaKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		writeError(w, http.StatusBadRequest, "certificate does not contain an RSA private key")
		return
	}
	// Zero the RSA key object after we've finished using it.
	defer signer.ZeroKey(rsaKey)

	// Validate ARRL certificate chain (unless explicitly skipped for dev/test).
	if !s.skipChainVerify {
		if err := signer.VerifyARRLChain(cert, caChain); err != nil {
			log.Printf("[vault] import-cert chain verify failed for user %q: %v", userID, err)
			writeError(w, http.StatusBadRequest, "certificate does not chain to ARRL LoTW trust store")
			return
		}
	}

	// Require the ARRL callsign OID — no CommonName fallback.
	callsign, err := signer.ExtractCallsign(cert)
	if err != nil {
		log.Printf("[vault] import-cert callsign extract failed for user %q: %v", userID, err)
		writeError(w, http.StatusBadRequest, "not a valid ARRL LoTW certificate")
		return
	}

	// Extract ARRL-specific extensions.
	qsoStart, qsoEnd, dxcc := signer.ExtractARRLExtensions(cert)

	// Check cert validity window.
	now := time.Now()
	if now.After(cert.NotAfter) {
		writeError(w, http.StatusBadRequest, "certificate has expired")
		return
	}

	// Encrypt private key using Argon2id-derived AES-256-GCM key.
	salt, err := vaultcrypto.GenerateSalt()
	if err != nil {
		log.Printf("[vault] import-cert generate salt for user %q: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	derivedKey := vaultcrypto.DeriveKey(userPassword, salt)
	defer vaultcrypto.ZeroBytes(derivedKey)

	keyDER, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		log.Printf("[vault] import-cert marshal private key for user %q: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer vaultcrypto.ZeroBytes(keyDER)

	encryptedKey, err := vaultcrypto.Encrypt(derivedKey, keyDER)
	if err != nil {
		log.Printf("[vault] import-cert encrypt private key for user %q: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Encode CA chain as concatenated DER blobs (if present).
	var caChainDER []byte
	for _, ca := range caChain {
		caChainDER = append(caChainDER, ca.Raw...)
	}

	entry := &certstore.EncryptedEntry{
		Meta: certstore.CertMeta{
			UserID:     userID,
			Callsign:   callsign,
			DXCC:       dxcc,
			QSOStart:   qsoStart,
			QSOEnd:     qsoEnd,
			NotBefore:  cert.NotBefore,
			NotAfter:   cert.NotAfter,
			CertDER:    cert.Raw,
			CAChainDER: caChainDER,
		},
		EncryptedKey:  encryptedKey,
		Argon2Salt:    salt,
		Argon2Time:    2,
		Argon2Memory:  64 * 1024,
		Argon2Threads: 4,
	}

	if err := s.store.Save(entry); err != nil {
		log.Printf("[vault] import-cert save for user %q: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, importResponse{
		Callsign:  callsign,
		DXCC:      dxcc,
		QSOStart:  qsoStart,
		QSOEnd:    qsoEnd,
		NotBefore: cert.NotBefore.Format(time.RFC3339),
		NotAfter:  cert.NotAfter.Format(time.RFC3339),
		Message:   "Certificate imported successfully",
	})
}

// ── POST /sign ────────────────────────────────────────────────────────────────

type signRequest struct {
	UserID       string             `json:"user_id"`
	UserPassword string             `json:"user_password"`
	ADIFData     string             `json:"adif_data"`
	Station      stationInfoRequest `json:"station"`
}

type stationInfoRequest struct {
	Callsign   string `json:"callsign"`
	DXCC       string `json:"dxcc"`
	Gridsquare string `json:"gridsquare"`
	ITUZ       string `json:"ituz"`
	CQZ        string `json:"cqz"`
	IOTA       string `json:"iota"`
	Country    string `json:"country"`
	USState    string `json:"us_state"`
	USCounty   string `json:"us_county"`
	CAProvince string `json:"ca_province"`
}

func (s *Server) handleSign(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	var req signRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.UserPassword == "" {
		writeError(w, http.StatusBadRequest, "user_password is required")
		return
	}
	if req.ADIFData == "" {
		writeError(w, http.StatusBadRequest, "adif_data is required")
		return
	}

	// Parse ADIF strictly — any malformed tag is a hard rejection.
	records, err := adif.ParseAll(req.ADIFData)
	if err != nil {
		log.Printf("[vault] sign ADIF parse error for user %q: %v", req.UserID, err)
		writeError(w, http.StatusBadRequest, "malformed ADIF input")
		return
	}
	if len(records) == 0 {
		writeError(w, http.StatusBadRequest, "no QSO records found in adif_data")
		return
	}

	// Load and decrypt the stored certificate.
	entry, err := s.store.Load(req.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "authentication failed")
		return
	}

	// Verify the cert hasn't expired.
	if time.Now().After(entry.Meta.NotAfter) {
		writeError(w, http.StatusBadRequest, "certificate has expired")
		return
	}

	// Derive key using the stored KDF parameters (not hardcoded constants).
	derivedKey := vaultcrypto.DeriveKeyWithParams(
		req.UserPassword, entry.Argon2Salt,
		entry.Argon2Time, entry.Argon2Memory, entry.Argon2Threads,
	)
	defer vaultcrypto.ZeroBytes(derivedKey)

	keyDER, err := vaultcrypto.Decrypt(derivedKey, entry.EncryptedKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}
	defer vaultcrypto.ZeroBytes(keyDER)

	privKeyIface, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		log.Printf("[vault] sign parse private key for user %q: %v", req.UserID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	rsaKey, ok := privKeyIface.(*rsa.PrivateKey)
	if !ok {
		log.Printf("[vault] sign stored key not RSA for user %q", req.UserID)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	qsos := make([]signer.QSO, 0, len(records))
	for _, rec := range records {
		qso := signer.QSO{
			Call:     rec["CALL"],
			Band:     rec["BAND"],
			BandRX:   rec["BAND_RX"],
			Freq:     rec["FREQ"],
			FreqRX:   rec["FREQ_RX"],
			Mode:     rec["MODE"],
			Submode:  rec["SUBMODE"],
			PropMode: rec["PROP_MODE"],
			SatName:  rec["SAT_NAME"],
			QSODate:  rec["QSO_DATE"],
			QSOTime:  rec["TIME_ON"],
		}
		// Some ADIF exporters use QSO_TIME, others TIME_ON.
		if qso.QSOTime == "" {
			qso.QSOTime = rec["QSO_TIME"]
		}
		if qso.Call == "" || qso.Band == "" || qso.Mode == "" || qso.QSODate == "" || qso.QSOTime == "" {
			continue // skip incomplete QSO records (valid ADIF, just missing required fields)
		}
		qsos = append(qsos, qso)
	}

	if len(qsos) == 0 {
		writeError(w, http.StatusBadRequest, "no complete QSO records (CALL, BAND, MODE, QSO_DATE, QSO_TIME all required)")
		return
	}

	// Use station info from request, falling back to cert metadata.
	callsign := req.Station.Callsign
	if callsign == "" {
		callsign = entry.Meta.Callsign
	}
	dxcc := req.Station.DXCC
	if dxcc == "" {
		dxcc = entry.Meta.DXCC
	}

	station := signer.StationInfo{
		Callsign:   callsign,
		DXCC:       dxcc,
		Gridsquare: req.Station.Gridsquare,
		ITUZ:       req.Station.ITUZ,
		CQZ:        req.Station.CQZ,
		IOTA:       req.Station.IOTA,
		Country:    req.Station.Country,
		USState:    req.Station.USState,
		USCounty:   req.Station.USCounty,
		CAProvince: req.Station.CAProvince,
	}

	// BuildTQ8 zeroes the private key after use.
	tq8Data, err := signer.BuildTQ8(entry.Meta.CertDER, rsaKey, station, qsos)
	if err != nil {
		log.Printf("[vault] sign build tq8 for user %q: %v", req.UserID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Return the gzip-compressed .tq8 as binary.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tq8"`, callsign))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tq8Data)
}

// ── POST /rotate-password ─────────────────────────────────────────────────────

type rotatePasswordRequest struct {
	UserID      string `json:"user_id"`
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleRotatePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	var req rotatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.OldPassword == "" {
		writeError(w, http.StatusBadRequest, "old_password is required")
		return
	}
	if req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "new_password is required")
		return
	}
	if req.OldPassword == req.NewPassword {
		writeError(w, http.StatusBadRequest, "new_password must differ from old_password")
		return
	}

	// Load the current entry.
	entry, err := s.store.Load(req.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "authentication failed")
		return
	}

	// Decrypt with old password using stored KDF params.
	oldDerivedKey := vaultcrypto.DeriveKeyWithParams(
		req.OldPassword, entry.Argon2Salt,
		entry.Argon2Time, entry.Argon2Memory, entry.Argon2Threads,
	)
	defer vaultcrypto.ZeroBytes(oldDerivedKey)

	keyDER, err := vaultcrypto.Decrypt(oldDerivedKey, entry.EncryptedKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}
	defer vaultcrypto.ZeroBytes(keyDER)

	// Generate fresh salt for the new password.
	newSalt, err := vaultcrypto.GenerateSalt()
	if err != nil {
		log.Printf("[vault] rotate-password generate salt for user %q: %v", req.UserID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	newDerivedKey := vaultcrypto.DeriveKey(req.NewPassword, newSalt)
	defer vaultcrypto.ZeroBytes(newDerivedKey)

	newEncryptedKey, err := vaultcrypto.Encrypt(newDerivedKey, keyDER)
	if err != nil {
		log.Printf("[vault] rotate-password re-encrypt for user %q: %v", req.UserID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Update the entry with new encrypted key, salt, and default KDF params.
	entry.EncryptedKey = newEncryptedKey
	entry.Argon2Salt = newSalt
	entry.Argon2Time = 2
	entry.Argon2Memory = 64 * 1024
	entry.Argon2Threads = 4

	if err := s.store.Save(entry); err != nil {
		log.Printf("[vault] rotate-password save for user %q: %v", req.UserID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "password rotated successfully"})
}

// ── DELETE /cert ──────────────────────────────────────────────────────────────

type deleteCertRequest struct {
	UserID       string `json:"user_id"`
	UserPassword string `json:"user_password"`
}

func (s *Server) handleDeleteCert(w http.ResponseWriter, r *http.Request) {
	// Bound the request body to prevent denial-of-service via unbounded reads.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB

	var req deleteCertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.UserPassword == "" {
		writeError(w, http.StatusBadRequest, "user_password is required")
		return
	}

	// Load and try to decrypt to verify ownership before deleting.
	entry, err := s.store.Load(req.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "authentication failed")
		return
	}

	derivedKey := vaultcrypto.DeriveKeyWithParams(
		req.UserPassword, entry.Argon2Salt,
		entry.Argon2Time, entry.Argon2Memory, entry.Argon2Threads,
	)
	defer vaultcrypto.ZeroBytes(derivedKey)

	// Attempt decryption to verify password before allowing deletion.
	keyDER, err := vaultcrypto.Decrypt(derivedKey, entry.EncryptedKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}
	vaultcrypto.ZeroBytes(keyDER)

	if err := s.store.Delete(req.UserID); err != nil {
		log.Printf("[vault] delete cert for user %q: %v", req.UserID, err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "certificate deleted"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
