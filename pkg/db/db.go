package db

import "database/sql"

var DB *sql.DB

func Connect(dsn string) error {
	// we will implement PostgreSQL connection next step
	return nil
}
