package regressionmysql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/lp/campus-market/internal/app"
	"github.com/lp/campus-market/internal/config"
	"github.com/lp/campus-market/internal/database"
	"github.com/lp/campus-market/internal/testsupport"
	gmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func jsonUnmarshal(raw []byte, target any) error { return json.Unmarshal(raw, target) }

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return fmt.Sprintf("%d", port)
}

func waitUntilListening(t *testing.T, port string) {
	t.Helper()
	url := "http://127.0.0.1:" + port + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("healthz status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server never became available on %s: %v", url, lastErr)
}

func assertNotListening(t *testing.T, port string) {
	t.Helper()
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("service became available on port %s despite the failed startup migration", port)
	}
}

// TestEnsureWidthErrorIdentifiesField verifies against the real server that a
// failed widening is wrapped into ErrReportNoteColumnWidth and that the error
// names the report note field directly. Here the reports table is absent, so
// MariaDB returns a genuine Error 1146.
func TestEnsureWidthErrorIdentifiesField(t *testing.T) {
	db := freshDB(t) // scratch database, no tables created
	err := database.EnsureReportHandleResultWidth(db)
	if err == nil {
		t.Fatalf("expected widening failure against missing reports table")
	}
	if !errors.Is(err, database.ErrReportNoteColumnWidth) {
		t.Fatalf("error must wrap ErrReportNoteColumnWidth, got: %v", err)
	}
	if !strings.Contains(err.Error(), "reports.handle_result") {
		t.Fatalf("error must point at the report note field reports.handle_result, got: %v", err)
	}
	var myErr *mysqldriver.MySQLError
	if !errors.As(err, &myErr) || myErr.Number != 1146 {
		t.Fatalf("expected real MySQL Error 1146 in the chain, got: %v", err)
	}
	t.Logf("field-identifiable migration error: %v", err)
}

// TestStartupAbortsWhenReportNoteColumnWideningFails proves the startup
// ordering contract: when reports.handle_result cannot be widened, app.Start
// returns an error wrapping ErrReportNoteColumnWidth that names the field,
// returns no Application and the HTTP port never opens. After the failure is
// cleared, the same database starts and long-note handling works end to end.
//
// The schema is migrated in sync first, so GORM AutoMigrate is a no-op; a GORM
// callback fails ONLY the explicit widening statement (matched by its DDL),
// isolating the production failure point precisely.
func TestStartupAbortsWhenReportNoteColumnWideningFails(t *testing.T) {
	rootDB := freshDB(t)
	testsupport.MigrateAndSeed(t, rootDB)

	// Seed a pending report that must remain unhandled while startup is blocked.
	seedApp := testsupport.New(t, rootDB)
	pid := publishProduct(t, seedApp)
	rid := fileReport(t, seedApp, pid, "fraud")

	dsn := scratchDSN(t, currentDatabase(t, rootDB))
	port := freePort(t)
	cfg := &config.Config{
		DSN: dsn, Port: port, JWTSecret: "test-secret", JWTExpireHours: 24,
		RateLimitPerMin: 10000, LoginRateLimit: 10000,
		CORSOrigins: []string{"*"}, SeedingEnabled: false,
	}

	// blockWidening=1 makes the connection reject the report-note ALTER only.
	var blockWidening atomic.Bool
	blockWidening.Store(true)
	openDB := func(dsn string) (*gorm.DB, error) {
		gdb, err := gorm.Open(gmysql.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, err
		}
		_ = gdb.Callback().Raw().Before("gorm:raw").Register("test_fail_report_note_widen", func(tx *gorm.DB) {
			if blockWidening.Load() && strings.Contains(tx.Statement.SQL.String(), "MODIFY COLUMN handle_result") {
				tx.AddError(fmt.Errorf("Error 1142 (42000): ALTER command denied for reports.handle_result"))
			}
		})
		return gdb, nil
	}
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	application, startErr := app.Start(cfg, discard, openDB, nil)
	if application != nil {
		_ = application.Shutdown(2 * time.Second)
		t.Fatalf("Start must not return an application when report-note widening fails")
	}
	if startErr == nil {
		t.Fatalf("expected startup failure when reports.handle_result cannot be widened")
	}
	if !errors.Is(startErr, database.ErrReportNoteColumnWidth) {
		t.Fatalf("startup error must wrap ErrReportNoteColumnWidth, got: %v", startErr)
	}
	if !strings.Contains(startErr.Error(), "reports.handle_result") {
		t.Fatalf("startup error must name the report note field, got: %v", startErr)
	}
	t.Logf("startup aborted with identifiable error: %v", startErr)
	assertNotListening(t, port)

	// Schema untouched: the report is still pending and the product on sale.
	var status, pstatus string
	if err := rootDB.Table("reports").Where("id = ?", rid).Select("status").Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if err := rootDB.Table("products").Where("id = ?", pid).Select("status").Scan(&pstatus).Error; err != nil {
		t.Fatal(err)
	}
	if status != "pending" || pstatus != "on_sale" {
		t.Fatalf("state changed during aborted startup: report=%s product=%s", status, pstatus)
	}

	// Clear the fault and restart: startup succeeds and the service is live.
	blockWidening.Store(false)
	application, startErr = app.Start(cfg, discard, openDB, nil)
	if startErr != nil {
		t.Fatalf("startup after recovery must succeed: %v", startErr)
	}
	defer func() { _ = application.Shutdown(2 * time.Second) }()
	waitUntilListening(t, port)

	// The formerly-blocked long-note handling now works through the live HTTP API.
	base := "http://127.0.0.1:" + port
	tokenAdmin := loginOverHTTP(t, base, "13800000001", "admin123")
	body := fmt.Sprintf(`{"action":"remove","note":%q}`, strings.Repeat("长", 200))
	code, raw := httpJSON(t, http.MethodPost, base+"/api/v1/admin/reports/"+itoa(rid)+"/handle", tokenAdmin, body)
	if code != http.StatusOK {
		t.Fatalf("long-note handling after recovery status = %d body=%s", code, raw)
	}
}

// TestStartupAbortsOnRealAlterDenied uses a genuine permission failure: the
// application account has no ALTER privilege and the report column is still at
// the old width. Startup must abort (1142 ALTER denied) and never listen.
func TestStartupAbortsOnRealAlterDenied(t *testing.T) {
	rootDB := freshDB(t)
	dbName := currentDatabase(t, rootDB)
	testsupport.MigrateAndSeed(t, rootDB)
	simulateOldSchema(t, rootDB) // handle_result -> VARCHAR(128)

	adminRoot, err := gorm.Open(gmysql.Open(rootDSN()), &gorm.Config{})
	if err != nil {
		t.Skipf("admin connect unavailable: %v", err)
	}
	user := fmt.Sprintf("regnopriv_%d", dbSeq)
	pass := "noalter_pwd"
	stmts := []string{
		fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", user, pass),
		fmt.Sprintf("GRANT SELECT,INSERT,UPDATE,DELETE,CREATE,INDEX,REFERENCES ON `%s`.* TO '%s'@'%%'", dbName, user),
		"FLUSH PRIVILEGES",
	}
	for _, s := range stmts {
		if err := adminRoot.Exec(s).Error; err != nil {
			t.Skipf("server does not support account provisioning (%v): %s", err, s)
		}
	}
	defer func() { _ = adminRoot.Exec(fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user)) }()

	userDSN := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, pass, mysqlHostPort(), dbName)

	port := freePort(t)
	appCfg := &config.Config{
		DSN: userDSN, Port: port, JWTSecret: "test-secret", JWTExpireHours: 24,
		RateLimitPerMin: 10000, LoginRateLimit: 10000,
		CORSOrigins: []string{"*"}, SeedingEnabled: false,
	}
	openDB := func(dsn string) (*gorm.DB, error) {
		return gorm.Open(gmysql.Open(dsn), &gorm.Config{})
	}
	application, startErr := app.Start(appCfg, slog.New(slog.NewTextHandler(io.Discard, nil)), openDB, nil)
	if application != nil {
		_ = application.Shutdown(2 * time.Second)
		t.Fatalf("Start must not return an application when ALTER is denied")
	}
	if startErr == nil || !strings.Contains(startErr.Error(), "ALTER command denied") {
		t.Fatalf("expected real ALTER-denied failure, got: %v", startErr)
	}
	t.Logf("startup aborted on real privilege failure: %v", startErr)
	assertNotListening(t, port)
}

// --- small HTTP helpers driving the live server started by app.Start ---

func httpJSON(t *testing.T, method, url, token, body string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, url, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func loginOverHTTP(t *testing.T, base, phone, password string) string {
	t.Helper()
	status, raw := httpJSON(t, http.MethodPost, base+"/api/v1/users/login", "",
		fmt.Sprintf(`{"phone":%q,"password":%q}`, phone, password))
	if status != http.StatusOK {
		t.Fatalf("login %s status=%d body=%s", phone, status, raw)
	}
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := jsonUnmarshal(raw, &env); err != nil || env.Data.Token == "" {
		t.Fatalf("login decode: %s", raw)
	}
	return env.Data.Token
}
