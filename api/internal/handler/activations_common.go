package handler

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/FtlC-ian/radioledger/api/internal/auth"
	db "github.com/FtlC-ian/radioledger/api/internal/database/sqlc"
)

var (
	potaReferencePattern = regexp.MustCompile(`^[A-Z]{1,3}-[0-9]{1,5}$`)
	sotaReferencePattern = regexp.MustCompile(`^[A-Z0-9]{1,3}/[A-Z]{2}-[0-9]{3}$`)
)

const (
	activationProgramPOTA = "POTA"
	activationProgramSOTA = "SOTA"
)

type activationCreateRequest struct {
	Reference           string  `json:"reference"`
	ActivationDate      string  `json:"activation_date"`
	LogbookUUID         *string `json:"logbook_uuid,omitempty"`
	StationLocationUUID *string `json:"station_location_uuid,omitempty"`
	Notes               *string `json:"notes,omitempty"`
}

type activationUpdateRequest struct {
	Reference           *string `json:"reference,omitempty"`
	ActivationDate      *string `json:"activation_date,omitempty"`
	StationLocationUUID *string `json:"station_location_uuid,omitempty"`
	Notes               *string `json:"notes,omitempty"`
	Status              *string `json:"status,omitempty"`
}

type activationQSOResponse struct {
	UUID            string   `json:"uuid"`
	Callsign        string   `json:"callsign"`
	Band            string   `json:"band"`
	Mode            string   `json:"mode"`
	Submode         *string  `json:"submode,omitempty"`
	DatetimeOn      string   `json:"datetime_on"`
	RstSent         *string  `json:"rst_sent,omitempty"`
	RstRcvd         *string  `json:"rst_rcvd,omitempty"`
	FrequencyHz     *int64   `json:"frequency_hz,omitempty"`
	SotaRef         *string  `json:"sota_ref,omitempty"`
	MySotaRef       *string  `json:"my_sota_ref,omitempty"`
	Sig             *string  `json:"sig,omitempty"`
	SigInfo         *string  `json:"sig_info,omitempty"`
	PotaRefs        []string `json:"pota_refs,omitempty"`
	MyPotaRefs      []string `json:"my_pota_refs,omitempty"`
	StationCallsign string   `json:"station_callsign"`
	MyGridsquare    string   `json:"my_gridsquare"`
	Comment         *string  `json:"comment,omitempty"`
	Notes           *string  `json:"notes,omitempty"`
}

type activationStatusResponse struct {
	Program               string   `json:"program"`
	Reference             string   `json:"reference"`
	ActivationDate        string   `json:"activation_date"`
	Status                string   `json:"status"`
	QSOCount              int64    `json:"qso_count"`
	UniqueCallsigns       int64    `json:"unique_callsigns"`
	MinimumContacts       int64    `json:"minimum_contacts"`
	ContactsNeeded        int64    `json:"contacts_needed"`
	MissingRequiredFields []string `json:"missing_required_fields"`
	S2SCount              int64    `json:"s2s_count,omitempty"`
	Warnings              []string `json:"warnings,omitempty"`
	ReadyToSubmit         bool     `json:"ready_to_submit"`
}

type activationResponse struct {
	UUID                string                   `json:"uuid"`
	LogbookUUID         string                   `json:"logbook_uuid"`
	Program             string                   `json:"program"`
	Reference           string                   `json:"reference"`
	ActivationDate      string                   `json:"activation_date"`
	StationLocationUUID *string                  `json:"station_location_uuid,omitempty"`
	Notes               *string                  `json:"notes,omitempty"`
	Status              string                   `json:"status"`
	QSOCount            int64                    `json:"qso_count"`
	UniqueCallsigns     int64                    `json:"unique_callsigns"`
	CreatedAt           string                   `json:"created_at"`
	UpdatedAt           string                   `json:"updated_at"`
	Validation          activationStatusResponse `json:"validation"`
}

func parseActivationDate(value string) (pgtype.Date, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = time.Now().UTC().Format("2006-01-02")
	}
	t, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return pgtype.Date{}, fmt.Errorf("activation_date must be YYYY-MM-DD")
	}
	return pgtype.Date{Time: t, Valid: true}, nil
}

func formatDate(value pgtype.Date) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format("2006-01-02")
}

func normalizeActivationReference(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func validateActivationReference(program, reference string) error {
	normalized := normalizeActivationReference(reference)
	switch strings.ToUpper(strings.TrimSpace(program)) {
	case activationProgramPOTA:
		if !potaReferencePattern.MatchString(normalized) {
			return fmt.Errorf("invalid POTA reference format, expected like K-1234")
		}
	case activationProgramSOTA:
		if !sotaReferencePattern.MatchString(normalized) {
			return fmt.Errorf("invalid SOTA reference format, expected like W4C/WM-001")
		}
	}
	return nil
}

func parseActivationStatus(value string, fallback string) (string, error) {
	v := strings.TrimSpace(strings.ToLower(value))
	if v == "" {
		if strings.TrimSpace(fallback) == "" {
			return "in_progress", nil
		}
		return strings.TrimSpace(fallback), nil
	}
	switch v {
	case "in_progress", "valid", "submitted":
		return v, nil
	default:
		return "", fmt.Errorf("status must be one of: in_progress, valid, submitted")
	}
}

func stationLocationUUIDString(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	u, err := uuid.FromBytes(value.Bytes[:])
	if err != nil {
		return nil
	}
	s := u.String()
	return &s
}

func resolveActivationLogbook(
	ctx context.Context,
	queries *db.Queries,
	userID int64,
	rawLogbookUUID *string,
	permission auth.Permission,
) (logbookID int64, logbookUUID uuid.UUID, err error) {
	if rawLogbookUUID == nil || strings.TrimSpace(*rawLogbookUUID) == "" {
		row, err := queries.GetDefaultLogbook(ctx, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, uuid.Nil, fmt.Errorf("default logbook not found")
			}
			return 0, uuid.Nil, err
		}
		if _, err := ensureLogbookPermission(ctx, queries, userID, row.Uuid, permission); err != nil {
			if errors.Is(err, errForbiddenRBAC) {
				return 0, uuid.Nil, fmt.Errorf("insufficient permissions for logbook")
			}
			return 0, uuid.Nil, err
		}
		return row.ID, row.Uuid, nil
	}

	parsed, err := uuid.Parse(strings.TrimSpace(*rawLogbookUUID))
	if err != nil {
		return 0, uuid.Nil, fmt.Errorf("invalid logbook UUID")
	}

	row, err := queries.GetLogbookByUUID(ctx, parsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, uuid.Nil, fmt.Errorf("logbook not found")
		}
		return 0, uuid.Nil, err
	}
	if _, err := ensureLogbookPermission(ctx, queries, userID, row.Uuid, permission); err != nil {
		if errors.Is(err, errForbiddenRBAC) {
			return 0, uuid.Nil, fmt.Errorf("insufficient permissions for logbook")
		}
		return 0, uuid.Nil, err
	}
	return row.ID, row.Uuid, nil
}

func resolveStationLocationID(
	ctx context.Context,
	queries *db.Queries,
	raw *string,
	fallback *int64,
) (*int64, error) {
	if raw == nil {
		return fallback, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	locationUUID, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid station_location_uuid")
	}
	row, err := queries.GetStationLocationByUUID(ctx, locationUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("station location not found")
		}
		return nil, err
	}
	return &row.ID, nil
}

func computeActivationValidation(program string, statusRow db.GetActivationStatusByUUIDAndProgramRow) activationStatusResponse {
	minimum := int64(10)
	if strings.EqualFold(program, activationProgramSOTA) {
		minimum = 4
	}

	contactsNeeded := minimum - statusRow.UniqueCallsigns
	if contactsNeeded < 0 {
		contactsNeeded = 0
	}

	missing := make([]string, 0, 2)
	warnings := make([]string, 0, 3)

	if strings.EqualFold(program, activationProgramPOTA) {
		if statusRow.MissingStationCallsign > 0 {
			missing = append(missing, "STATION_CALLSIGN")
		}
		if statusRow.MissingMyGridsquare > 0 {
			missing = append(missing, "MY_GRIDSQUARE")
		}
		if statusRow.UniqueCallsigns < minimum {
			warnings = append(warnings, fmt.Sprintf("POTA requires at least %d unique callsigns", minimum))
		}
	}

	if strings.EqualFold(program, activationProgramSOTA) && statusRow.S2sCount == 0 {
		warnings = append(warnings, "No S2S QSOs detected. SOTA recommends at least one summit-to-summit contact when possible.")
	}

	ready := statusRow.UniqueCallsigns >= minimum && len(missing) == 0
	computedStatus := strings.TrimSpace(statusRow.Status)
	if strings.EqualFold(computedStatus, "submitted") {
		ready = true
	} else if ready {
		computedStatus = "valid"
	} else {
		computedStatus = "in_progress"
	}

	return activationStatusResponse{
		Program:               statusRow.Program,
		Reference:             statusRow.Reference,
		ActivationDate:        formatDate(statusRow.ActivationDate),
		Status:                computedStatus,
		QSOCount:              statusRow.QsoCount,
		UniqueCallsigns:       statusRow.UniqueCallsigns,
		MinimumContacts:       minimum,
		ContactsNeeded:        contactsNeeded,
		MissingRequiredFields: missing,
		S2SCount:              statusRow.S2sCount,
		Warnings:              warnings,
		ReadyToSubmit:         ready,
	}
}

func activationQSOFromRow(row db.ListActivationQSOsByUUIDAndProgramRow) activationQSOResponse {
	return activationQSOResponse{
		UUID:            row.Uuid.String(),
		Callsign:        row.Callsign,
		Band:            row.Band,
		Mode:            row.Mode,
		Submode:         row.Submode,
		DatetimeOn:      row.DatetimeOn.Time.UTC().Format(time.RFC3339),
		RstSent:         row.RstSent,
		RstRcvd:         row.RstRcvd,
		FrequencyHz:     row.FrequencyHz,
		SotaRef:         row.SotaRef,
		MySotaRef:       row.MySotaRef,
		Sig:             row.Sig,
		SigInfo:         row.SigInfo,
		PotaRefs:        row.PotaRefs,
		MyPotaRefs:      row.MyPotaRefs,
		StationCallsign: row.StationCallsignExport,
		MyGridsquare:    row.MyGridsquareExport,
		Comment:         row.Comment,
		Notes:           row.Notes,
	}
}

func activationResponseFromLookupRow(row db.GetActivationByUUIDAndProgramRow, validation activationStatusResponse) activationResponse {
	return activationResponse{
		UUID:                row.Uuid.String(),
		LogbookUUID:         row.LogbookUuid.String(),
		Program:             row.Program,
		Reference:           row.Reference,
		ActivationDate:      formatDate(row.ActivationDate),
		StationLocationUUID: stationLocationUUIDString(row.StationLocationUuid),
		Notes:               row.Notes,
		Status:              validation.Status,
		QSOCount:            validation.QSOCount,
		UniqueCallsigns:     validation.UniqueCallsigns,
		CreatedAt:           row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		Validation:          validation,
	}
}

func activationResponseFromListRow(row db.ListActivationsByProgramRow, validation activationStatusResponse) activationResponse {
	return activationResponse{
		UUID:                row.Uuid.String(),
		LogbookUUID:         row.LogbookUuid.String(),
		Program:             row.Program,
		Reference:           row.Reference,
		ActivationDate:      formatDate(row.ActivationDate),
		StationLocationUUID: stationLocationUUIDString(row.StationLocationUuid),
		Notes:               row.Notes,
		Status:              validation.Status,
		QSOCount:            validation.QSOCount,
		UniqueCallsigns:     validation.UniqueCallsigns,
		CreatedAt:           row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:           row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		Validation:          validation,
	}
}
