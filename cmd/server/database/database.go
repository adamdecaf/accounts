// Copyright 2020 The Moov Authors
// Use of this source code is governed by an Apache License
// license that can be found in the LICENSE file.

package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	kitprom "github.com/go-kit/kit/metrics/prometheus"
	"github.com/lopezator/migrator"
)

func New(ctx context.Context, logger log.Logger, _type string) (*sql.DB, error) {
	logger.Log("database", fmt.Sprintf("looking for %s database provider", _type))
	switch strings.ToLower(_type) {
	case "sqlite", "":
		return SQLiteConnection(logger, SQLitePath()).Connect(ctx)
	case "mysql":
		return mysqlConnection(logger, os.Getenv("MYSQL_USER"), os.Getenv("MYSQL_PASSWORD"), os.Getenv("MYSQL_ADDRESS"), os.Getenv("MYSQL_DATABASE")).Connect(ctx)
	}
	return nil, fmt.Errorf("unknown database type %q", _type)
}

func execsql(name, raw string) *migrator.MigrationNoTx {
	return &migrator.MigrationNoTx{
		Name: name,
		Func: func(db *sql.DB) error {
			_, err := db.Exec(raw)
			return err
		},
	}
}

// UniqueViolation returns true when the provided error matches a database error
// for duplicate entries (violating a unique table constraint).
func UniqueViolation(err error) bool {
	return MySQLUniqueViolation(err) || SqliteUniqueViolation(err)
}

func recordStatus(metric *kitprom.Gauge, db *sql.DB) {
	stats := db.Stats()
	metric.With("state", "idle").Set(float64(stats.Idle))
	metric.With("state", "inuse").Set(float64(stats.InUse))
	metric.With("state", "open").Set(float64(stats.OpenConnections))
}