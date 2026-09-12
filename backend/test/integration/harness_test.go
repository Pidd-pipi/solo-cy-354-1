package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqle "github.com/dolthub/go-mysql-server"
	"github.com/dolthub/go-mysql-server/memory"
	"github.com/dolthub/go-mysql-server/server"
	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/information_schema"
	"github.com/lp/campus-market/internal/testsupport"
	gmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// startInMemoryMySQL launches an in-process MySQL-compatible server and
// returns its address (host:port). No Docker/MySQL binary is required.
func startInMemoryMySQL(t *testing.T) string {
	t.Helper()
	// information_schema is required so the GORM migrator can detect existing
	// tables, making repeated AutoMigrate (as run by app.Start) idempotent.
	provider := sql.NewDatabaseProvider(
		memory.NewDatabase("lpcampusmarket_db"),
		information_schema.NewInformationSchemaDatabase(),
	)
	engine := sqle.NewDefault(provider)
	srv, err := server.NewDefaultServer(server.Config{
		Protocol: "tcp",
		Address:  "127.0.0.1:0",
	}, engine)
	if err != nil {
		t.Fatalf("start in-memory mysql: %v", err)
	}
	go func() { _ = srv.Start() }()
	addr := srv.Listener.Addr().String()
	t.Cleanup(func() { _ = srv.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if db, err := gorm.Open(gmysql.Open(dsn(addr)), &gorm.Config{}); err == nil {
			if sqlDB, e := db.DB(); e == nil {
				_ = sqlDB.Ping()
				sqlDB.Close()
				return addr
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("in-memory mysql at %s not ready", addr)
	return addr
}

func dsn(addr string) string {
	return fmt.Sprintf("root:@tcp(%s)/lpcampusmarket_db?charset=utf8mb4&parseTime=True&loc=Local", addr)
}

// harness wires the real Gin router against a database for in-memory
// integration tests. It reuses the shared testsupport application assembly.
type harness struct {
	t       *testing.T
	app     *testsupport.App
	router  http.Handler
	db      *gorm.DB
	tokenA  string
	tokenB  string
	adminTo string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := gorm.Open(gmysql.Open(dsn(startInMemoryMySQL(t))), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	return newHarnessWithDB(t, db)
}

func newHarnessWithDB(t *testing.T, db *gorm.DB) *harness {
	t.Helper()
	testsupport.MigrateAndSeed(t, db)
	app := testsupport.New(t, db)
	return &harness{
		t: t, app: app, router: app.Handler, db: db,
		tokenA: app.TokenA, tokenB: app.TokenB, adminTo: app.TokenC,
	}
}

type envelope = testsupport.Envelope

func (h *harness) login(phone, password string) string {
	return h.app.Login(phone, password)
}

func (h *harness) do(method, path, token string, body []byte) *httptest.ResponseRecorder {
	reader := bytes.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *harness) mustData(rec *httptest.ResponseRecorder, where string) json.RawMessage {
	h.t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		h.t.Fatalf("%s: bad json: %v body=%s", where, err, rec.Body.String())
	}
	if env.Code != 0 {
		h.t.Fatalf("%s: code=%d message=%s", where, env.Code, env.Message)
	}
	return env.Data
}

func (h *harness) assertBusinessError(rec *httptest.ResponseRecorder, wantCode int, where string) {
	h.t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		h.t.Fatalf("%s: bad json: %v body=%s", where, err, rec.Body.String())
	}
	if env.Code != wantCode {
		h.t.Fatalf("%s: code=%d want %d body=%s", where, env.Code, wantCode, rec.Body.String())
	}
}
