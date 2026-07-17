// Package lotw provides LoTW upload functionality intended for use by the
// main RadioLedger API — NOT the signing vault itself.
//
// The vault has no internet access by design (its Docker network is internal-only).
// After the vault returns a signed .tq8 blob, the main API is responsible for
// forwarding it to ARRL using this helper.
package lotw

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	// UploadURL is the ARRL LoTW upload endpoint.
	UploadURL = "https://lotw.arrl.org/lotw/upload"
	// UploadField is the multipart field name expected by ARRL.
	UploadField = "upfile"
)

// UploadResult holds the parsed result of an ARRL upload attempt.
type UploadResult struct {
	// Accepted is true when ARRL confirmed the upload was accepted.
	Accepted bool
	// RawResponse is the full HTML body returned by ARRL.
	RawResponse string
}

// Upload submits a .tq8 blob to ARRL's LoTW.
// Returns an UploadResult describing whether ARRL accepted the log.
//
// Note: This function makes an outbound HTTPS request to lotw.arrl.org.
// Call it from the main RadioLedger API, not from the signing vault.
func Upload(tq8Data []byte) (*UploadResult, error) {
	if len(tq8Data) == 0 {
		return nil, fmt.Errorf("tq8_data must not be empty")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(UploadField, "upload.tq8")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(tq8Data); err != nil {
		return nil, fmt.Errorf("write form file: %w", err)
	}
	mw.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(UploadURL, mw.FormDataContentType(), &buf)
	if err != nil {
		return nil, fmt.Errorf("arrl upload POST: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	bodyStr := string(body)

	return &UploadResult{
		Accepted:    strings.Contains(bodyStr, "<!-- .UPL. accepted -->"),
		RawResponse: bodyStr,
	}, nil
}
