package db

import (
	"fmt"

	"github/hchw/kianshu/internal/config"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Open opens a GORM connection for the configured dialect.
func Open(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DBDriver {
	case "sqlite":
		dialector = sqlite.Open(cfg.DSN)
	case "mysql":
		dialector = mysql.Open(cfg.DSN)
	case "postgres", "pgsql", "postgresql":
		dialector = postgres.Open(cfg.DSN)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", cfg.DBDriver)
	}
	return gorm.Open(dialector, &gorm.Config{})
}
