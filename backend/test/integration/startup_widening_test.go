package integration

import (
	"errors"
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

// wideningGuard intercepts ONLY the report-note column widening statement and,
// while "deny" is on, makes the driver return the same structured MySQL error
// the server returns when the application account lacks ALTER privilege
// (Error 1142 / SQLSTATE 42000). No database account, GRANT or administrator
// credential is involved — the test therefore runs against the privilege-free
// in-memory MySQL engine.
type wideningGuard struct {
	deny         *atomic.Bool
	blockedSQL   string
	blockedCount int
}

// openDB returns an app.OpenDBFunc that installs the guard on every
// connection opened during startup.
func (g *wideningGuard) openDB(addrDSN string) app.OpenDBFunc {
	return func(_ string) (*gorm.DB, error) {
		gdb, err := gorm.Open(gmysql.Open(addrDSN), &gorm.Config{})
		if err != nil {
			return nil, err
		}
		// Exact statement identity (no backticks) distinguishes the explicit
		// migration DDL from GORM migrator-generated ALTER statements.
		_ = gdb.Callback().Raw().Before("gorm:raw").Register("deny_report_note_widen", func(tx *gorm.DB) {
			if g == nil || g.deny == nil || !g.deny.Load() || tx.Statement == nil {
				return
			}
			sql := strings.TrimSpace(tx.Statement.SQL.String())
			if sql != database.ReportHandleResultDDL {
				return
			}
			g.blockedSQL = sql
			g.blockedCount++
			tx.AddError(&mysqldriver.MySQLError{
				Number:   1142, // ER_TABLEACCESS_DENIED_ERROR — same as real privilege denial
				SQLState: [5]byte{'4', '2', '0', '0', '0'},
				Message:  "ALTER command denied for reports.handle_result widening",
			})
		})
		// Upgrade-scenario shim: the schema (tables + indexes) already exists,
		// so treat the in-memory engine's incomplete index introspection —
		// which otherwise re-issues CREATE INDEX for pre-existing indexes — as
		// an idempotent no-op, exactly like MySQL behaves when the index is
		// already there. Table/column creation is still genuinely executed.
		_ = gdb.Callback().Raw().After("gorm:raw").Register("idempotent_existing_indexes", func(tx *gorm.DB) {
			if tx.Error == nil || tx.Statement == nil || tx.Statement.SQL.Len() == 0 {
				return
			}
			sql := strings.ToUpper(tx.Statement.SQL.String())
			if strings.Contains(sql, "CREATE") && strings.Contains(sql, "INDEX") &&
				strings.Contains(tx.Error.Error(), "index already exists") {
				tx.Error = nil
			}
		})
		return gdb, nil
	}
}

func reservePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return itoa(uint(port))
}

func mustNotListen(t *testing.T, port string) {
	t.Helper()
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("service started accepting requests on %s although migration failed", port)
	}
}

func waitHealthy(t *testing.T, port string) {
	t.Helper()
	url := "http://127.0.0.1:" + port + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("service never became healthy on %s", url)
}

// TestStartupAbortsWhenReportNoteWideningDeniedAndRecovers verifies, without
// requiring a privileged database account, that:
//  1. when the reports.handle_result widening is rejected with MySQL 1142,
//     app.Start returns an error wrapping the typed ErrReportNoteColumnWidth
//     sentinel AND carrying the driver-typed 1142 error (structural assertions,
//     not string matches),
//  2. the blocked statement is exactly the report-note widening DDL and the
//     error names reports.handle_result,
//  3. no Application is returned and the service never accepts HTTP requests,
//  4. pending report / product state is untouched while startup is blocked,
//  5. once the privilege failure is cleared, the SAME database starts
//     successfully and a 200-character handling note works end to end.
func TestStartupAbortsWhenReportNoteWideningDeniedAndRecovers(t *testing.T) {
	addr := startInMemoryMySQL(t)
	addrDSN := dsn(addr)

	// Prepare the same schema/seed production would have; AutoMigrate creates
	// the reports table, then the explicit widening is what gets denied.
	rootDB, err := gorm.Open(gmysql.Open(addrDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	testsupport.MigrateAndSeed(t, rootDB)

	// A pending report against an on-sale product must survive the aborted boot.
	seed := testsupport.New(t, rootDB)
	pid := publishViaApp(t, seed)
	rid := reportViaApp(t, seed, pid)

	port := reservePort(t)
	cfg := &config.Config{
		DSN: addrDSN, Port: port, JWTSecret: "test-secret", JWTExpireHours: 24,
		RateLimitPerMin: 10000, LoginRateLimit: 10000,
		CORSOrigins: []string{"*"}, SeedingEnabled: false,
	}
	guard := &wideningGuard{}
	guard.deny = &atomic.Bool{}
	guard.deny.Store(true)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// ---- Boot must fail: widening rejected with privilege-denied error ----
	application, startErr := app.Start(cfg, logger, guard.openDB(addrDSN), nil)
	if application != nil {
		_ = application.Shutdown(2 * time.Second)
		t.Fatalf("app.Start returned an application despite the denied widening")
	}
	if startErr == nil {
		t.Fatalf("app.Start succeeded although reports.handle_result widening was denied")
	}

	// Structural: the feature sentinel identifies the blocked field.
	if !errors.Is(startErr, database.ErrReportNoteColumnWidth) {
		t.Fatalf("startup error must wrap ErrReportNoteColumnWidth, got: %v", startErr)
	}
	// Structural: the real MySQL privilege-denied error survives the chain,
	// so operators can react to error code 1142 programmatically.
	var myErr *mysqldriver.MySQLError
	if !errors.As(startErr, &myErr) || myErr.Number != 1142 || myErr.SQLState != [5]byte{'4', '2', '0', '0', '0'} {
		t.Fatalf("startup error must carry MySQL 1142/42000, got: %v", startErr)
	}
	// The blocked statement is exactly the report-note widening DDL.
	if guard.blockedCount != 1 || guard.blockedSQL != database.ReportHandleResultDDL {
		t.Fatalf("widening DDL not intercepted as expected: count=%d sql=%q", guard.blockedCount, guard.blockedSQL)
	}
	if !strings.Contains(startErr.Error(), "reports.handle_result") {
		t.Fatalf("error must name the blocked field, got: %v", startErr)
	}
	t.Logf("startup aborted: %v", startErr)

	// The service must not accept any request.
	mustNotListen(t, port)

	// State untouched: report still pending, product still on sale.
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

	// ---- Recovery: privilege fixed, the SAME database boots successfully ----
	guard.deny.Store(false)
	application, startErr = app.Start(cfg, logger, guard.openDB(addrDSN), nil)
	if startErr != nil {
		t.Fatalf("startup after the privilege is restored must succeed: %v", startErr)
	}
	defer func() { _ = application.Shutdown(2 * time.Second) }()
	waitHealthy(t, port)

	// The formerly blocked long-note handling now works through the live API.
	base := "http://127.0.0.1:" + port
	adminToken := login(t, base, "13800000001", "admin123")
	body := `{"action":"remove","note":"` + strings.Repeat("复", 200) + `"}`
	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/admin/reports/"+itoa(rid)+"/handle", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("handle after recovery: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw := make([]byte, 4096)
		n, _ := resp.Body.Read(raw)
		t.Fatalf("long-note handling after recovery status=%d body=%s", resp.StatusCode, raw[:n])
	}
}

// publishViaApp/reportViaApp drive the in-process API to seed a pending report.
func publishViaApp(t *testing.T, a *testsupport.App) uint {
	t.Helper()
	rec := a.Do(http.MethodPost, "/api/v1/products", a.TokenA, testsupport.JSON(map[string]any{
		"title": "启动迁移失败商品", "description": "d", "price": 7,
		"category": "books", "condition": "全新", "campus": "东校区", "trade_location": "东门",
	}))
	var p struct {
		ID uint `json:"id"`
	}
	if err := jsonUnmarshalData(rec.Body.Bytes(), &p); err != nil || p.ID == 0 {
		t.Fatalf("publish: %s", rec.Body.String())
	}
	return p.ID
}

func reportViaApp(t *testing.T, a *testsupport.App, pid uint) uint {
	t.Helper()
	rec := a.Do(http.MethodPost, "/api/v1/reports", a.TokenB,
		testsupport.JSON(map[string]any{"product_id": pid, "reason": "fraud"}))
	var r struct {
		ID uint `json:"id"`
	}
	if err := jsonUnmarshalData(rec.Body.Bytes(), &r); err != nil || r.ID == 0 {
		t.Fatalf("report: %s", rec.Body.String())
	}
	return r.ID
}
