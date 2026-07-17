// adif.go — ADIF record builder for LoTW signing.
//
// BuildADIF converts a slice of QSO rows into an ADIF string suitable for
// submission to the vault's /sign endpoint.
package lotw

import (
	"bytes"
	"fmt"
	"time"

	adifpkg "github.com/FtlC-ian/radioledger/pkg/adif"
)

// LoTWQSORow holds the QSO fields needed to produce a LoTW ADIF record.
// All pointer fields are optional; nil values are omitted from the ADIF output.
type LoTWQSORow struct {
	// QSOID is the internal database primary key, included as APP_RADIOLEDGER_ID
	// to correlate signed records back to DB rows after upload.
	QSOID int64

	Callsign        string
	Band            string
	Mode            string
	Submode         *string
	DatetimeOn      time.Time
	RstSent         *string
	RstRcvd         *string
	Gridsquare      *string // their gridsquare
	MyGridsquare    *string
	StationCallsign *string
	FrequencyHz     *int64
}

// BuildADIF formats a slice of QSO rows as an ADIF string for LoTW signing.
// Each QSO becomes a single ADIF record. The output is passed directly to the
// vault's /sign endpoint as the adif_data field.
//
// APP_RADIOLEDGER_ID is appended to each record so the signed .tq8 can be
// correlated back to RadioLedger QSO IDs after ARRL acceptance.
func BuildADIF(rows []LoTWQSORow) (string, error) {
	var buf bytes.Buffer
	w := adifpkg.NewWriter(&buf)

	for _, row := range rows {
		rec := adifpkg.Record{}
		rec.Fields = []adifpkg.Field{
			{Name: "CALL", Value: row.Callsign},
			{Name: "BAND", Value: row.Band},
			{Name: "MODE", Value: row.Mode},
			{Name: "QSO_DATE", Value: row.DatetimeOn.UTC().Format("20060102")},
			{Name: "TIME_ON", Value: row.DatetimeOn.UTC().Format("1504")},
		}
		if row.Submode != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "SUBMODE", Value: *row.Submode})
		}
		if row.RstSent != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "RST_SENT", Value: *row.RstSent})
		}
		if row.RstRcvd != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "RST_RCVD", Value: *row.RstRcvd})
		}
		if row.Gridsquare != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "GRIDSQUARE", Value: *row.Gridsquare})
		}
		if row.MyGridsquare != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "MY_GRIDSQUARE", Value: *row.MyGridsquare})
		}
		if row.StationCallsign != nil {
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "STATION_CALLSIGN", Value: *row.StationCallsign})
		}
		if row.FrequencyHz != nil {
			mhz := float64(*row.FrequencyHz) / 1_000_000.0
			rec.Fields = append(rec.Fields, adifpkg.Field{Name: "FREQ", Value: fmt.Sprintf("%.6f", mhz)})
		}
		adifpkg.CanonicalizeRecordMode(&rec)
		// APP_RADIOLEDGER_ID is a vendor extension field; ARRL ignores unknown APP_ fields.
		rec.Fields = append(rec.Fields, adifpkg.Field{
			Name:  "APP_RADIOLEDGER_ID",
			Value: fmt.Sprintf("%d", row.QSOID),
		})

		if err := w.WriteRecord(&rec); err != nil {
			return "", fmt.Errorf("write ADIF record for qso %d: %w", row.QSOID, err)
		}
	}

	return buf.String(), nil
}
