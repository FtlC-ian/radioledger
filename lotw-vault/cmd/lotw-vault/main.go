// Command lotw-vault is the LoTW Signing Vault microservice.
//
// Usage:
//
//	lotw-vault [flags]
//
// Flags:
//
//	-addr string       Listen address (default ":8443")
//	-db string         Path to SQLite database file (default "/data/certs.db")
//	-data string       Fallback: filesystem cert directory (used only if -db is empty)
//	-tls-cert          Path to TLS certificate file
//	-tls-key           Path to TLS key file
//	-test-sign         Sign a sample ADIF and write output.tq8 (for development testing)
//	-p12               Path to .p12 file (used with -test-sign)
//	-p12-password      Password for the .p12 file
//	-adif              Path to ADIF file (used with -test-sign)
//	-callsign          Station callsign (used with -test-sign)
//	-grid              Station gridsquare (used with -test-sign)
//	-dxcc              Station DXCC entity (used with -test-sign)
package main

import (
	"crypto/rsa"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"software.sslmate.com/src/go-pkcs12"

	"github.com/FtlC-ian/radioledger/lotw-vault/internal/adif"
	"github.com/FtlC-ian/radioledger/lotw-vault/internal/certstore"
	"github.com/FtlC-ian/radioledger/lotw-vault/internal/server"
	"github.com/FtlC-ian/radioledger/lotw-vault/internal/signer"
)

func main() {
	addr := flag.String("addr", ":8443", "listen address")
	dbPath := flag.String("db", "/data/certs.db", "path to SQLite database file")
	dataDir := flag.String("data", "", "fallback filesystem cert directory (deprecated; use -db)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (optional)")
	tlsKey := flag.String("tls-key", "", "TLS key file (optional)")
	skipChainVerify := flag.Bool("skip-chain-verify", false, "UNSAFE: skip ARRL certificate chain verification (development only)")

	// Test-sign flags (for development / manual verification)
	testSign := flag.Bool("test-sign", false, "sign a sample ADIF file and exit")
	p12Path := flag.String("p12", "", "path to .p12 certificate file")
	p12Password := flag.String("p12-password", "", "password for .p12 file")
	adifPath := flag.String("adif", "", "path to ADIF file to sign")
	callsign := flag.String("callsign", "", "station callsign")
	grid := flag.String("grid", "", "station gridsquare")
	dxcc := flag.String("dxcc", "291", "station DXCC entity")
	country := flag.String("country", "", "station country")
	cqz := flag.String("cqz", "", "CQ zone")
	ituz := flag.String("ituz", "", "ITU zone")
	outputPath := flag.String("output", "output.tq8", "output .tq8 file path (for -test-sign)")

	flag.Parse()

	if *testSign {
		if err := runTestSign(*p12Path, *p12Password, *adifPath, *outputPath, *callsign, *grid, *dxcc, *country, *cqz, *ituz); err != nil {
			log.Fatalf("test-sign failed: %v", err)
		}
		return
	}

	// Choose storage backend. SQLite is preferred; filesystem is the fallback.
	var store certstore.Store
	if *dataDir != "" {
		log.Printf("Warning: -data is deprecated; prefer -db for SQLite storage")
		fs, err := certstore.NewFilesystemStore(*dataDir)
		if err != nil {
			log.Fatalf("init filesystem store: %v", err)
		}
		store = fs
	} else {
		sq, err := certstore.NewSQLiteStore(*dbPath)
		if err != nil {
			log.Fatalf("init sqlite store at %s: %v", *dbPath, err)
		}
		defer sq.Close()
		store = sq
		log.Printf("Using SQLite store at %s", *dbPath)
	}

	var srvOpts []server.Option
	if *skipChainVerify {
		log.Printf("WARNING: ARRL certificate chain verification is DISABLED (-skip-chain-verify). Do not use in production.")
		srvOpts = append(srvOpts, server.WithSkipChainVerify())
	}
	srv := server.New(store, srvOpts...)

	httpSrv := &http.Server{
		Addr:         *addr,
		Handler:      srv.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if *tlsCert != "" && *tlsKey != "" {
		httpSrv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		log.Printf("LoTW Vault listening on %s (TLS)", *addr)
		if err := httpSrv.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil {
			log.Fatalf("server error: %v", err)
		}
	} else {
		log.Printf("LoTW Vault listening on %s (plain HTTP — use TLS in production!)", *addr)
		if err := httpSrv.ListenAndServe(); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}
}

// runTestSign is a development helper: it loads a .p12, parses an ADIF file,
// signs all QSOs, and writes the resulting .tq8 for manual verification with:
//
//	gunzip -c output.tq8 | less
func runTestSign(p12Path, p12Password, adifPath, outputPath, callsignArg, grid, dxcc, country, cqz, ituz string) error {
	if p12Path == "" {
		return fmt.Errorf("-p12 is required for -test-sign")
	}
	if adifPath == "" {
		return fmt.Errorf("-adif is required for -test-sign")
	}

	p12Data, err := os.ReadFile(p12Path)
	if err != nil {
		return fmt.Errorf("read p12: %w", err)
	}

	privKeyIface, cert, _, err := pkcs12.DecodeChain(p12Data, p12Password)
	if err != nil {
		return fmt.Errorf("decode p12: %w", err)
	}
	rsaKey, ok := privKeyIface.(*rsa.PrivateKey)
	if !ok {
		return fmt.Errorf("p12 private key is not RSA")
	}

	callsign, err := signer.ExtractCallsign(cert)
	if err != nil {
		// Fall back to argument
		if callsignArg == "" {
			return fmt.Errorf("extract callsign: %w", err)
		}
		callsign = callsignArg
	}
	if callsignArg != "" {
		callsign = callsignArg
	}

	_, _, certDXCC := signer.ExtractARRLExtensions(cert)
	if dxcc == "291" && certDXCC != "" {
		dxcc = certDXCC
	}

	adifData, err := os.ReadFile(adifPath)
	if err != nil {
		return fmt.Errorf("read adif: %w", err)
	}

	records, err := adif.ParseAll(string(adifData))
	if err != nil {
		return fmt.Errorf("parse adif: %w", err)
	}
	fmt.Printf("Parsed %d ADIF records\n", len(records))

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
		if qso.QSOTime == "" {
			qso.QSOTime = rec["QSO_TIME"]
		}
		if qso.Call == "" || qso.Band == "" || qso.Mode == "" || qso.QSODate == "" || qso.QSOTime == "" {
			fmt.Printf("  Skipping incomplete record: %v\n", rec)
			continue
		}
		qsos = append(qsos, qso)
	}

	if len(qsos) == 0 {
		return fmt.Errorf("no complete QSO records found")
	}
	fmt.Printf("Signing %d QSOs as %s\n", len(qsos), callsign)

	station := signer.StationInfo{
		Callsign:   callsign,
		DXCC:       dxcc,
		Gridsquare: grid,
		CQZ:        cqz,
		ITUZ:       ituz,
		Country:    country,
	}

	tq8Data, err := signer.BuildTQ8(cert.Raw, rsaKey, station, qsos)
	if err != nil {
		return fmt.Errorf("build tq8: %w", err)
	}

	if err := os.WriteFile(outputPath, tq8Data, 0600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Printf("Written %d bytes to %s\n", len(tq8Data), outputPath)
	fmt.Printf("Verify with: gunzip -c %s | head -50\n", outputPath)
	return nil
}
