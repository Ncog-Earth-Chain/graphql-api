// Package migrate applies the explorer's PostgreSQL schema migrations.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"ncogearthchain-api-graphql/internal/logger"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrations are embedded in the binary rather than read from disk.
//
// A binary that carries its own schema cannot be deployed against the wrong migration
// directory, and there is no ordering question between "deploy the code" and "copy the
// SQL". It also means the migration state is exactly reproducible from a commit.
//
//go:embed sql/*.sql
var migrations embed.FS

// Up applies every pending migration.
//
// It takes a DSN rather than the pgx pool because goose speaks database/sql. The
// stdlib adapter wraps the same pgx driver, so there is no second driver in the build.
//
// Migrations run BEFORE the pool opens, and a failure is fatal to startup: serving
// requests against a half-migrated schema produces errors that look like data
// corruption, and the first symptom would be a query referencing a column that does
// not exist yet.
func Up(ctx context.Context, dsn string, log logger.Logger) error {
	db, err := openForMigration(dsn)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Errorf("can not close migration connection; %s", err.Error())
		}
	}()

	goose.SetBaseFS(migrations)
	goose.SetLogger(gooseLogger{log})

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("can not set migration dialect: %w", err)
	}

	before, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("can not read schema version: %w", err)
	}

	if err := goose.UpContext(ctx, db, "sql"); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	after, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("can not read schema version after migrating: %w", err)
	}

	if after == before {
		log.Debugf("database schema up to date at version %d", after)
	} else {
		log.Noticef("database schema migrated from version %d to %d", before, after)
	}
	return nil
}

// Version reports the current schema version.
func Version(ctx context.Context, dsn string) (int64, error) {
	db, err := openForMigration(dsn)
	if err != nil {
		return 0, err
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return 0, err
	}
	return goose.GetDBVersionContext(ctx, db)
}

// openForMigration opens a single database/sql connection over the pgx driver.
func openForMigration(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid PostgreSQL DSN: %w", err)
	}

	db := stdlib.OpenDB(*cfg)

	// One connection. Migrations are serial by nature, and goose takes an advisory
	// lock to keep concurrent instances from racing -- a lock that is only meaningful
	// if every statement of the run uses the same session.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return db, nil
}

// gooseLogger routes goose output into the application logger, so migration output
// lands in the same place as everything else rather than on stdout.
type gooseLogger struct{ log logger.Logger }

func (g gooseLogger) Fatalf(format string, v ...interface{}) { g.log.Criticalf(format, v...) }
func (g gooseLogger) Printf(format string, v ...interface{}) { g.log.Debugf(format, v...) }
