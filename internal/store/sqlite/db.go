package sqlite

import (
	"embed"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nulzo/model-router-api/internal/store"
)

//go:embed migrations/*.sql
var fs embed.FS

func NewSQLiteStorage(dsn string) (store.Repository, error) {
	// add required pragmas for performance if not present
	// such as: dsn = "file:router.db?cache=shared&mode=rwc&_journal_mode=WAL&_busy_timeout=5000"
	db, err := sqlx.Connect("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return NewSqliteRepository(db), nil
}

func runMigrations(db *sqlx.DB) error {
	driver, err := sqlite3.WithInstance(db.DB, &sqlite3.Config{})
	if err != nil {
		return err
	}

	d, err := iofs.New(fs, "migrations")
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", d, "sqlite3", driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	log.Println("Database migrations applied successfully")
	return nil
}
