// arrl_upload.go — submits a signed .tq8 blob to ARRL's LoTW upload endpoint.
//
// The vault does NOT upload to ARRL (its Docker network has no internet access).
// After receiving a .tq8 from the vault /sign endpoint, the main API is
// responsible for forwarding it to ARRL using UploadToARRL.
//
// Reference implementation: lotw-vault/internal/lotw/upload.go
package lotw

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	// ARRLUploadURL is the ARRL LoTW upload endpoint.
	ARRLUploadURL = "https://lotw.arrl.org/lotw/upload"
	// arrlUploadField is the multipart field name expected by ARRL.
	arrlUploadField = "upfile"
	// arrlUploadAcceptedMarker is the HTML comment ARRL embeds in accepted responses.
	arrlUploadAcceptedMarker = "<!-- .UPL. accepted -->"
)

// ARRLUploadResult holds the parsed result of an ARRL upload attempt.
type ARRLUploadResult struct {
	// Accepted is true when ARRL confirmed the upload was accepted.
	Accepted bool
	// RawResponse is the full HTML body returned by ARRL.
	// Useful for debugging or storing in the lotw_sync_jobs.arrl_response column.
	RawResponse string
}

// UploadToARRL submits a signed .tq8 blob to ARRL's LoTW upload endpoint.
// Returns an ARRLUploadResult describing whether ARRL accepted the log.
//
// ARRL's response is plain HTML, not JSON. Acceptance is detected by the
// presence of the "<!-- .UPL. accepted -->" comment in the response body.
//
// Note: This function makes an outbound HTTPS request to lotw.arrl.org.
// Call it from the main RadioLedger API, not from the signing vault.
func UploadToARRL(ctx context.Context, tq8Data []byte) (*ARRLUploadResult, error) {
	if len(tq8Data) == 0 {
		return nil, fmt.Errorf("tq8_data must not be empty")
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(arrlUploadField, "upload.tq8")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(tq8Data); err != nil {
		return nil, fmt.Errorf("write tq8 to form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ARRLUploadURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("build ARRL upload request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ARRL upload POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	bodyStr := string(body)

	return &ARRLUploadResult{
		Accepted:    strings.Contains(bodyStr, arrlUploadAcceptedMarker),
		RawResponse: bodyStr,
	}, nil
}
