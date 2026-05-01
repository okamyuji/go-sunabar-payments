// Package database アプリ全体で使う MySQL 接続を構築するヘルパを提供する。
package database

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	// MySQL ドライバを匿名 import で登録する。 sql.Open ( "mysql", dsn ) を可能にする。
	_ "github.com/go-sql-driver/mysql"
)

// Config 接続設定。 DSN 直指定が無ければ DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME から組み立てる。
type Config struct {
	DSN          string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
}

// FromEnv 環境変数から Config を構築する。
// DB_DSN または DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME を参照する。
func FromEnv() Config {
	cfg := Config{
		DSN:          os.Getenv("DB_DSN"),
		MaxOpenConns: 20,
		MaxIdleConns: 5,
		MaxLifetime:  5 * time.Minute,
	}
	if cfg.DSN != "" {
		return cfg
	}
	host := getOr("DB_HOST", "127.0.0.1")
	port := getOr("DB_PORT", "3306")
	user := getOr("DB_USER", "app")
	pass := getOr("DB_PASSWORD", "app")
	name := getOr("DB_NAME", "payments")
	cfg.DSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC&multiStatements=true",
		user, pass, host, port, name)
	if v := os.Getenv("DB_MAX_OPEN_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxOpenConns = n
		}
	}
	return cfg
}

// Open *sql.DB を開いて疎通を確認する。
func Open(cfg Config) (*sql.DB, error) {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxLifetime)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return db, nil
}

func getOr(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
