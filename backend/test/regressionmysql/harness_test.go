// Package regressionmysql contains behavioral regression tests that MUST run
// against a real MySQL-compatible server (InnoDB, strict SQL mode): column
// width enforcement and transaction rollback cannot be observed with the
// in-memory test engines (they ignore VARCHAR limits and ROLLBACK).
//
// Provide a server via TEST_MYSQL_DSN (a DSN WITHOUT a database name):
//
//	TEST_MYSQL_DSN='root@tcp(127.0.0.1:13308)/?charset=utf8mb4&parseTime=True' \
//	  go test ./test/regressionmysql/ -v
//
// Each test creates its own scratch database and drops it on cleanup, so the
// suite is fully repeatable. When no server is reachable the tests skip.
package regressionmysql

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/lp/campus-market/internal/testsupport"
	gmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var dbSeq uint64

func rootDSN() string {
	if v := os.Getenv("TEST_MYSQL_DSN"); v != "" {
		return v
	}
	return "root@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=True&loc=Local"
}

// freshDB creates a throwaway database on the real server and returns a GORM
// handle to it. The whole package is skipped when no server is reachable.
func freshDB(t *testing.T) *gorm.DB {
	t.Helper()
	root, err := gorm.Open(gmysql.Open(rootDSN()), &gorm.Config{})
	if err != nil {
		t.Skipf("real MySQL server not available (%v); set TEST_MYSQL_DSN to run this regression suite", err)
	}
	if sqlDB, err := root.DB(); err != nil {
		t.Skipf("real MySQL server pool unavailable: %v", err)
	} else if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		t.Skipf("real MySQL server not reachable (%v); set TEST_MYSQL_DSN to run this regression suite", err)
	}
	// Guarantee long-value rejection regardless of the server's defaults.
	if err := root.Exec("SET GLOBAL sql_mode='STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION'").Error; err != nil {
		t.Fatalf("set strict sql_mode: %v", err)
	}
	n := atomic.AddUint64(&dbSeq, 1)
	name := fmt.Sprintf("regression_%d_%d", time.Now().UnixNano(), n)
	if err := root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4").Error; err != nil {
		t.Fatalf("create scratch database: %v", err)
	}
	t.Cleanup(func() {
		_ = root.Exec("DROP DATABASE IF EXISTS `" + name + "`")
		if sqlDB, err := root.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	// Re-point the DSN at the scratch database.
	cfg, err := mysqldriver.ParseDSN(rootDSN())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.DBName = name
	db, err := gorm.Open(gmysql.Open(cfg.FormatDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open scratch database: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// columnType reads the real, database-enforced type of a column from
// information_schema (a behavioral metadata read, not a source/string check).
func columnType(t *testing.T, db *gorm.DB, table, column string) string {
	t.Helper()
	var typ string
	err := db.Raw(
		`SELECT COLUMN_TYPE FROM information_schema.COLUMNS
		 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		table, column).Scan(&typ).Error
	if err != nil {
		t.Fatalf("read column type %s.%s: %v", table, column, err)
	}
	return typ
}

// simulateOldSchema shrinks handle_result to the ORIGINAL VARCHAR(128) width
// deployed before the fix, reproducing an upgraded-but-not-yet-migrated DB.
func simulateOldSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ALTER TABLE reports MODIFY COLUMN handle_result VARCHAR(128) NOT NULL DEFAULT ''").Error; err != nil {
		t.Fatalf("simulate old 128-width schema: %v", err)
	}
	if got := columnType(t, db, "reports", "handle_result"); got != "varchar(128)" {
		t.Fatalf("old schema simulation failed, column type = %s", got)
	}
}

func isDataTooLong(err error) bool {
	var myErr *mysqldriver.MySQLError
	return errors.As(err, &myErr) && myErr.Number == 1406 || // ER_DATA_TOO_LONG
		err != nil && strings.Contains(err.Error(), "Data too long")
}

// fileReport creates a pending report through the real API and returns its id.
func fileReport(t *testing.T, app *testsupport.App, productID uint, reason string) uint {
	t.Helper()
	rec := app.Do(http.MethodPost, "/api/v1/reports", app.TokenB,
		testsupport.JSON(map[string]any{"product_id": productID, "reason": reason}))
	var data struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(app.MustData(rec, "file report"), &data); err != nil || data.ID == 0 {
		t.Fatalf("file report failed: %s", rec.Body.String())
	}
	return data.ID
}

// publishProduct creates a fresh on-sale product and returns its id.
func publishProduct(t *testing.T, app *testsupport.App) uint {
	t.Helper()
	rec := app.Do(http.MethodPost, "/api/v1/products", app.TokenA, testsupport.JSON(map[string]any{
		"title": "回归测试商品", "description": "d", "price": 8,
		"category": "books", "condition": "全新", "campus": "东校区", "trade_location": "东门",
	}))
	var p struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(app.MustData(rec, "publish"), &p); err != nil || p.ID == 0 {
		t.Fatalf("publish failed: %s", rec.Body.String())
	}
	return p.ID
}

// itoa renders a small positive id without pulling strconv into each test.
func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
