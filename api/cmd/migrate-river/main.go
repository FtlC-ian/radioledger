// Command migrate-river runs River's internal database migrations.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func main() {
	ctx := context.Background()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		log.Fatalf("migrator: %v", err)
	}

	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}

	for _, v := range res.Versions {
		fmt.Printf("Applied River migration version: %d\n", v.Version)
	}

	if err := applyRiverRoleGrants(ctx, pool); err != nil {
		log.Fatalf("grant river privileges: %v", err)
	}

	fmt.Println("River migration complete.")
}

func applyRiverRoleGrants(ctx context.Context, pool *pgxpool.Pool) error {
	// River tables/sequences are created by River's migrations in the public schema
	// (e.g. river_job, river_leader, river_migration). Our API request handlers run
	// inside SET LOCAL ROLE radioledger_api transactions, so enqueueing jobs requires
	// explicit grants on these objects.
	const grantSQL = `
DO $$
DECLARE
	has_api_role BOOLEAN := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_api');
	has_worker_role BOOLEAN := EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'radioledger_worker');
	t RECORD;
	s RECORD;
BEGIN
	FOR t IN
		SELECT schemaname, tablename
		FROM pg_tables
		WHERE schemaname = 'public' AND tablename LIKE 'river_%'
	LOOP
		IF has_api_role THEN
			EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO radioledger_api', t.schemaname, t.tablename);
		END IF;
		IF has_worker_role THEN
			EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I.%I TO radioledger_worker', t.schemaname, t.tablename);
		END IF;
	END LOOP;

	FOR s IN
		SELECT sequence_schema, sequence_name
		FROM information_schema.sequences
		WHERE sequence_schema = 'public' AND sequence_name LIKE 'river_%'
	LOOP
		IF has_api_role THEN
			EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO radioledger_api', s.sequence_schema, s.sequence_name);
		END IF;
		IF has_worker_role THEN
			EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I.%I TO radioledger_worker', s.sequence_schema, s.sequence_name);
		END IF;
	END LOOP;
END
$$;
`

	if _, err := pool.Exec(ctx, grantSQL); err != nil {
		return fmt.Errorf("apply river grants: %w", err)
	}

	fmt.Println("Applied River grants for radioledger_api/radioledger_worker roles.")
	return nil
}
