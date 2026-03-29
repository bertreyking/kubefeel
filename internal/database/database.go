package database

import (
	"multikube-manager/internal/config"
	"multikube-manager/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(cfg config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}

	if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(
		&model.Permission{},
		&model.Role{},
		&model.User{},
		&model.Cluster{},
		&model.ClusterProvisionJob{},
		&model.RegistryIntegration{},
		&model.ObservabilitySource{},
	); err != nil {
		return nil, err
	}

	return db, nil
}
