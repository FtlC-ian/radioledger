// Package migrations records the expected application schema migration state.
package migrations

// ConsolidatedLegacyGooseVersion is the version of the single consolidated
// 001_initial_schema.sql migration used by early non-Docker deployments, some
// of which applied the schema before app goose history was tracked.
const ConsolidatedLegacyGooseVersion int64 = 1

// ExpectedGooseVersion is the latest application goose migration version that
// this API build expects to find in goose_db_version.
//
// Keep this aligned with the highest numbered file under database/migrations.
// version_test.go fails if a new migration is added without updating this value.
const ExpectedGooseVersion int64 = 3
