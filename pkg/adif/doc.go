// Package adif provides a streaming ADIF (Amateur Data Interchange Format) parser
// and writer for ham radio log data.
//
// # Overview
//
// ADIF is the standard file format for ham radio log exchange. RadioLedger uses this
// package to import logs from external programs (WSJT-X, Ham Radio Deluxe, Log4OM, etc.)
// and to export logs for upload to LoTW, ClubLog, QRZ, and similar services.
//
// # Format
//
// The ADI format (handled here) uses angle-bracket tags for each field:
//
//	<FIELDNAME:LENGTH[:TYPE]>VALUE
//
// Fields are grouped into records, delimited by <EOR> (end of record).
// An optional header section precedes records, delimited by <EOH> (end of header).
//
// Example:
//
//	<ADIF_VER:5>3.1.4 <PROGRAMID:12>RadioLedger <EOH>
//	<CALL:4>W1AW <BAND:3>20m <MODE:3>SSB <QSO_DATE:8>20260228 <TIME_ON:6>153000 <EOR>
//
// # Security
//
// The parser is designed for untrusted input:
//   - Maximum field length is enforced (default 10 MB per field)
//   - Maximum record count is configurable (default 500,000)
//   - Parser supports cancellation via context.Context
//   - Random/malformed bytes never cause a panic (fuzz-tested)
//
// # Usage
//
//	f, _ := os.Open("log.adi")
//	p := adif.NewParser(f)
//	header, err := p.Header(ctx)
//	for {
//	    rec, err := p.Next(ctx)
//	    if err == io.EOF {
//	        break
//	    }
//	    // process rec
//	}
package adif
