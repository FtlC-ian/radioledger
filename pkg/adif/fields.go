package adif

// KnownFields is the catalog of ADIF 3.1.7 field names that RadioLedger handles.
// Fields not in this list are still parsed and passed through, they land in extra JSONB.
// This list is used for validation hints and canonical ordering in export.
var KnownFields = map[string]FieldDef{
	// Core contact fields
	"CALL":         {Name: "CALL", DataType: "S", Description: "Other station's callsign"},
	"BAND":         {Name: "BAND", DataType: "E", Description: "QSO band"},
	"BAND_RX":      {Name: "BAND_RX", DataType: "E", Description: "Receive band (split operation)"},
	"MODE":         {Name: "MODE", DataType: "E", Description: "QSO mode"},
	"SUBMODE":      {Name: "SUBMODE", DataType: "S", Description: "QSO submode"},
	"FREQ":         {Name: "FREQ", DataType: "N", Description: "QSO frequency in MHz"},
	"FREQ_RX":      {Name: "FREQ_RX", DataType: "N", Description: "Receive frequency in MHz"},
	"QSO_DATE":     {Name: "QSO_DATE", DataType: "D", Description: "QSO date (YYYYMMDD)"},
	"QSO_DATE_OFF": {Name: "QSO_DATE_OFF", DataType: "D", Description: "QSO end date (YYYYMMDD)"},
	"TIME_ON":      {Name: "TIME_ON", DataType: "T", Description: "QSO start time (HHMM or HHMMSS)"},
	"TIME_OFF":     {Name: "TIME_OFF", DataType: "T", Description: "QSO end time"},
	"RST_SENT":     {Name: "RST_SENT", DataType: "S", Description: "RST sent"},
	"RST_RCVD":     {Name: "RST_RCVD", DataType: "S", Description: "RST received"},
	"NAME":         {Name: "NAME", DataType: "S", Description: "Other station operator name"},
	"QTH":          {Name: "QTH", DataType: "S", Description: "Other station city/location"},
	"COMMENT":      {Name: "COMMENT", DataType: "S", Description: "Comment field"},
	"NOTES":        {Name: "NOTES", DataType: "S", Description: "Notes field"},
	"TX_PWR":       {Name: "TX_PWR", DataType: "N", Description: "Transmit power in watts"},
	"RX_PWR":       {Name: "RX_PWR", DataType: "N", Description: "Receive power in watts"},

	// Antenna and equipment
	"ANTENNA":    {Name: "ANTENNA", DataType: "S", Description: "Transmit antenna"},
	"MY_ANTENNA": {Name: "MY_ANTENNA", DataType: "S", Description: "My antenna"},
	"MY_RIG":     {Name: "MY_RIG", DataType: "S", Description: "My rig"},

	// Location — their station
	"GRIDSQUARE": {Name: "GRIDSQUARE", DataType: "G", Description: "Other station grid square"},
	"DXCC":       {Name: "DXCC", DataType: "I", Description: "Other station DXCC entity number"},
	"COUNTRY":    {Name: "COUNTRY", DataType: "S", Description: "Other station country"},
	"STATE":      {Name: "STATE", DataType: "E", Description: "Other station US state"},
	"COUNTY":     {Name: "COUNTY", DataType: "S", Description: "Other station county"},
	"CQZ":        {Name: "CQZ", DataType: "I", Description: "Other station CQ zone"},
	"ITUZ":       {Name: "ITUZ", DataType: "I", Description: "Other station ITU zone"},
	"CONT":       {Name: "CONT", DataType: "E", Description: "Continent of other station"},
	"LAT":        {Name: "LAT", DataType: "L", Description: "Latitude of other station"},
	"LON":        {Name: "LON", DataType: "L", Description: "Longitude of other station"},

	// Location — my station
	"MY_GRIDSQUARE": {Name: "MY_GRIDSQUARE", DataType: "G", Description: "My grid square"},
	"MY_CITY":       {Name: "MY_CITY", DataType: "S", Description: "My city"},
	"MY_STATE":      {Name: "MY_STATE", DataType: "E", Description: "My state"},
	"MY_COUNTRY":    {Name: "MY_COUNTRY", DataType: "S", Description: "My country"},
	"MY_DXCC":       {Name: "MY_DXCC", DataType: "I", Description: "My DXCC entity number"},
	"MY_CQ_ZONE":    {Name: "MY_CQ_ZONE", DataType: "I", Description: "My CQ zone"},
	"MY_ITU_ZONE":   {Name: "MY_ITU_ZONE", DataType: "I", Description: "My ITU zone"},
	"MY_LAT":        {Name: "MY_LAT", DataType: "L", Description: "My latitude"},
	"MY_LON":        {Name: "MY_LON", DataType: "L", Description: "My longitude"},

	// Propagation
	"SFI":       {Name: "SFI", DataType: "I", Description: "Solar flux index"},
	"A_INDEX":   {Name: "A_INDEX", DataType: "I", Description: "A-index"},
	"K_INDEX":   {Name: "K_INDEX", DataType: "I", Description: "K-index"},
	"PROP_MODE": {Name: "PROP_MODE", DataType: "E", Description: "Propagation mode"},

	// Operator / station identity
	"OPERATOR":         {Name: "OPERATOR", DataType: "S", Description: "Logging operator callsign"},
	"STATION_CALLSIGN": {Name: "STATION_CALLSIGN", DataType: "S", Description: "Callsign used for this QSO"},
	"OWNER_CALLSIGN":   {Name: "OWNER_CALLSIGN", DataType: "S", Description: "Callsign of licensee"},

	// Contest
	"CONTEST_ID": {Name: "CONTEST_ID", DataType: "S", Description: "Contest identifier"},
	"SRX":        {Name: "SRX", DataType: "I", Description: "Received serial number"},
	"STX":        {Name: "STX", DataType: "I", Description: "Transmitted serial number"},
	"SRX_STRING": {Name: "SRX_STRING", DataType: "S", Description: "Received contest exchange"},
	"STX_STRING": {Name: "STX_STRING", DataType: "S", Description: "Transmitted contest exchange"},

	// Satellite
	"SAT_NAME": {Name: "SAT_NAME", DataType: "S", Description: "Satellite name"},
	"SAT_MODE": {Name: "SAT_MODE", DataType: "S", Description: "Satellite mode"},

	// Awards and activities
	"SOTA_REF":    {Name: "SOTA_REF", DataType: "S", Description: "SOTA summit reference"},
	"MY_SOTA_REF": {Name: "MY_SOTA_REF", DataType: "S", Description: "My SOTA summit reference"},
	"POTA_REF":    {Name: "POTA_REF", DataType: "S", Description: "POTA park reference(s), comma-separated"},
	"MY_POTA_REF": {Name: "MY_POTA_REF", DataType: "S", Description: "My POTA park reference(s)"},
	"WWFF_REF":    {Name: "WWFF_REF", DataType: "S", Description: "WWFF reference"},
	"MY_WWFF_REF": {Name: "MY_WWFF_REF", DataType: "S", Description: "My WWFF reference"},
	"IOTA":        {Name: "IOTA", DataType: "I", Description: "IOTA island reference"},
	"SIG":         {Name: "SIG", DataType: "S", Description: "Special interest group"},
	"SIG_INFO":    {Name: "SIG_INFO", DataType: "S", Description: "Special interest group info"},

	// QSL — direct/bureau
	"QSL_SENT":     {Name: "QSL_SENT", DataType: "E", Description: "QSL sent status"},
	"QSL_SENT_VIA": {Name: "QSL_SENT_VIA", DataType: "E", Description: "QSL sent via"},
	"QSL_RCVD":     {Name: "QSL_RCVD", DataType: "E", Description: "QSL received status"},
	"QSL_RCVD_VIA": {Name: "QSL_RCVD_VIA", DataType: "E", Description: "QSL received via"},
	"QSL_VIA":      {Name: "QSL_VIA", DataType: "S", Description: "QSL manager callsign"},
	"QSLSDATE":     {Name: "QSLSDATE", DataType: "D", Description: "QSL sent date"},
	"QSLRDATE":     {Name: "QSLRDATE", DataType: "D", Description: "QSL received date"},

	// LoTW
	"LOTW_QSL_SENT": {Name: "LOTW_QSL_SENT", DataType: "E", Description: "LoTW QSL sent"},
	"LOTW_QSLSDATE": {Name: "LOTW_QSLSDATE", DataType: "D", Description: "LoTW QSL sent date"},
	"LOTW_QSL_RCVD": {Name: "LOTW_QSL_RCVD", DataType: "E", Description: "LoTW QSL received"},
	"LOTW_QSLRDATE": {Name: "LOTW_QSLRDATE", DataType: "D", Description: "LoTW QSL received date"},

	// eQSL
	"EQSL_QSL_SENT": {Name: "EQSL_QSL_SENT", DataType: "E", Description: "eQSL sent"},
	"EQSL_QSLSDATE": {Name: "EQSL_QSLSDATE", DataType: "D", Description: "eQSL sent date"},
	"EQSL_QSL_RCVD": {Name: "EQSL_QSL_RCVD", DataType: "E", Description: "eQSL received"},
	"EQSL_QSLRDATE": {Name: "EQSL_QSLRDATE", DataType: "D", Description: "eQSL received date"},
}

// FieldDef describes an ADIF field's metadata.
type FieldDef struct {
	Name        string
	DataType    string // S=string, N=number, D=date, T=time, E=enum, I=integer, G=grid, L=location
	Description string
}

// IsKnownField returns true if the field name is in the ADIF 3.1.7 known field catalog.
// APP_* namespace fields are always considered known.
func IsKnownField(name string) bool {
	if len(name) > 4 && name[:4] == "APP_" {
		return true
	}
	_, ok := KnownFields[name]
	return ok
}

// CanonicalFieldOrder is the deterministic export ordering for typed columns.
// Fields not in this list are appended alphabetically after.
var CanonicalFieldOrder = []string{
	"CALL",
	"QSO_DATE",
	"TIME_ON",
	"TIME_OFF",
	"QSO_DATE_OFF",
	"BAND",
	"MODE",
	"SUBMODE",
	"FREQ",
	"FREQ_RX",
	"BAND_RX",
	"RST_SENT",
	"RST_RCVD",
	"NAME",
	"QTH",
	"GRIDSQUARE",
	"DXCC",
	"COUNTRY",
	"STATE",
	"COUNTY",
	"CQZ",
	"ITUZ",
	"CONT",
	"LAT",
	"LON",
	"MY_GRIDSQUARE",
	"MY_CITY",
	"MY_STATE",
	"MY_COUNTRY",
	"MY_DXCC",
	"MY_CQ_ZONE",
	"MY_ITU_ZONE",
	"MY_LAT",
	"MY_LON",
	"TX_PWR",
	"RX_PWR",
	"MY_ANTENNA",
	"MY_RIG",
	"OPERATOR",
	"STATION_CALLSIGN",
	"OWNER_CALLSIGN",
	"PROP_MODE",
	"SFI",
	"A_INDEX",
	"K_INDEX",
	"CONTEST_ID",
	"SRX",
	"STX",
	"SRX_STRING",
	"STX_STRING",
	"SOTA_REF",
	"MY_SOTA_REF",
	"POTA_REF",
	"MY_POTA_REF",
	"WWFF_REF",
	"MY_WWFF_REF",
	"IOTA",
	"SIG",
	"SIG_INFO",
	"SAT_NAME",
	"SAT_MODE",
	"QSL_SENT",
	"QSL_SENT_VIA",
	"QSL_RCVD",
	"QSL_RCVD_VIA",
	"QSL_VIA",
	"QSLSDATE",
	"QSLRDATE",
	"LOTW_QSL_SENT",
	"LOTW_QSLSDATE",
	"LOTW_QSL_RCVD",
	"LOTW_QSLRDATE",
	"EQSL_QSL_SENT",
	"EQSL_QSLSDATE",
	"EQSL_QSL_RCVD",
	"EQSL_QSLRDATE",
	"COMMENT",
	"NOTES",
}

// KnownBands is the set of recognized amateur radio band names.
var KnownBands = map[string]BandDef{
	"2190m":  {Name: "2190m", LowerMHz: 0.1357, UpperMHz: 0.1378},
	"630m":   {Name: "630m", LowerMHz: 0.472, UpperMHz: 0.479},
	"560m":   {Name: "560m", LowerMHz: 0.501, UpperMHz: 0.504},
	"160m":   {Name: "160m", LowerMHz: 1.8, UpperMHz: 2.0},
	"80m":    {Name: "80m", LowerMHz: 3.5, UpperMHz: 4.0},
	"60m":    {Name: "60m", LowerMHz: 5.06, UpperMHz: 5.45},
	"40m":    {Name: "40m", LowerMHz: 7.0, UpperMHz: 7.3},
	"30m":    {Name: "30m", LowerMHz: 10.1, UpperMHz: 10.15},
	"20m":    {Name: "20m", LowerMHz: 14.0, UpperMHz: 14.35},
	"17m":    {Name: "17m", LowerMHz: 18.068, UpperMHz: 18.168},
	"15m":    {Name: "15m", LowerMHz: 21.0, UpperMHz: 21.45},
	"12m":    {Name: "12m", LowerMHz: 24.89, UpperMHz: 24.99},
	"10m":    {Name: "10m", LowerMHz: 28.0, UpperMHz: 29.7},
	"8m":     {Name: "8m", LowerMHz: 40.0, UpperMHz: 45.0},
	"6m":     {Name: "6m", LowerMHz: 50.0, UpperMHz: 54.0},
	"5m":     {Name: "5m", LowerMHz: 54.0, UpperMHz: 69.9},
	"4m":     {Name: "4m", LowerMHz: 70.0, UpperMHz: 71.0},
	"2m":     {Name: "2m", LowerMHz: 144.0, UpperMHz: 148.0},
	"1.25m":  {Name: "1.25m", LowerMHz: 222.0, UpperMHz: 225.0},
	"70cm":   {Name: "70cm", LowerMHz: 420.0, UpperMHz: 450.0},
	"33cm":   {Name: "33cm", LowerMHz: 902.0, UpperMHz: 928.0},
	"23cm":   {Name: "23cm", LowerMHz: 1240.0, UpperMHz: 1300.0},
	"13cm":   {Name: "13cm", LowerMHz: 2300.0, UpperMHz: 2450.0},
	"9cm":    {Name: "9cm", LowerMHz: 3300.0, UpperMHz: 3500.0},
	"6cm":    {Name: "6cm", LowerMHz: 5650.0, UpperMHz: 5925.0},
	"3cm":    {Name: "3cm", LowerMHz: 10000.0, UpperMHz: 10500.0},
	"1.25cm": {Name: "1.25cm", LowerMHz: 24000.0, UpperMHz: 24050.0},
	"6mm":    {Name: "6mm", LowerMHz: 47000.0, UpperMHz: 47200.0},
	"4mm":    {Name: "4mm", LowerMHz: 75500.0, UpperMHz: 81000.0},
	"2.5mm":  {Name: "2.5mm", LowerMHz: 119980.0, UpperMHz: 120020.0},
	"2mm":    {Name: "2mm", LowerMHz: 142000.0, UpperMHz: 149000.0},
	"1mm":    {Name: "1mm", LowerMHz: 241000.0, UpperMHz: 250000.0},
	"submm":  {Name: "submm", LowerMHz: 300000.0, UpperMHz: 7500000.0},
}

// BandDef describes an amateur radio band's frequency range.
type BandDef struct {
	Name     string
	LowerMHz float64
	UpperMHz float64
}
