// Package database handles the PostgreSQL connection lifecycle.
// Clean Architecture: infrastructure layer.
package database

import (
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens a GORM connection to PostgreSQL using the provided DSN.
// It auto-migrates the schema on startup.
func Connect(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	// Connection pool settings — tune for production load.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)

	log.Println("[database] connected to PostgreSQL")
	return db, nil
}

// AutoMigrate runs GORM auto-migration for the provided models.
// For production, prefer golang-migrate or goose SQL migrations in migrations/.
func AutoMigrate(db *gorm.DB, models ...interface{}) error {
	return db.AutoMigrate(models...)
}
