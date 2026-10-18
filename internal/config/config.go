package config

import (
	"time"
)

type Config struct {
}

type DatabaseConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	DBName          string
	SSLMode         string
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}
