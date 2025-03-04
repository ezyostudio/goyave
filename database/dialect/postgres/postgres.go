package postgres

import (
	"gorm.io/driver/postgres"
	"goyave.dev/goyave/v3/database"
)

func init() {
	database.RegisterDialect("postgres", "host={host} port={port} user={username} dbname={name} password={password} {options}", postgres.Open)
}
