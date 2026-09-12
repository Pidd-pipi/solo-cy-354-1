package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqle "github.com/dolthub/go-mysql-server"
	"github.com/dolthub/go-mysql-server/memory"
	"github.com/dolthub/go-mysql-server/server"
	"github.com/dolthub/go-mysql-server/sql"
	"github.com/lp/campus-market/internal/config"
	"github.com/lp/campus-market/internal/model"
	"github.com/lp/campus-market/internal/router"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// startInMemoryMySQL launches an in-process MySQL-compatible server and
// returns its address (host:port). No Docker/MySQL binary is required.
func startInMemoryMySQL(t *testing.T) string {
	t.Helper()
	provider := sql.NewDatabaseProvider(memory.NewDatabase("lpcampusmarket_db"))
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
	// Wait until the server accepts connections.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if db, err := gorm.Open(mysql.Open(dsn(addr, "root", "")), &gorm.Config{}); err == nil {
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

func dsn(addr, user, pass string) string {
	return fmt.Sprintf("%s:%s@tcp(%s)/lpcampusmarket_db?charset=utf8mb4&parseTime=True&loc=Local", user, pass, addr)
}

// harness wires the real Gin router against the in-memory MySQL and seeds
// three accounts: two students and one admin.
type harness struct {
	t       *testing.T
	router  http.Handler
	tokenA  string // student 13700000001
	tokenB  string // student 13700000002
	adminTo string // admin 13800000001
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	addr := startInMemoryMySQL(t)
	db, err := gorm.Open(mysql.Open(dsn(addr, "root", "")), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Product{}, &model.Conversation{}, &model.Message{},
		&model.TradeOrder{}, &model.Review{}, &model.BookExchange{}, &model.Report{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	mustHash := func(p string) string {
		h, _ := bcrypt.GenerateFromPassword([]byte(p), bcrypt.MinCost)
		return string(h)
	}
	users := []model.User{
		{Phone: "13700000001", PasswordHash: mustHash("123456"), Nickname: "学生甲", Role: "student", Campus: "东校区", CreditScore: 100},
		{Phone: "13700000002", PasswordHash: mustHash("123456"), Nickname: "学生乙", Role: "student", Campus: "西校区", CreditScore: 100},
		{Phone: "13800000001", PasswordHash: mustHash("admin123"), Nickname: "管理员", Role: "admin", Campus: "东校区", CreditScore: 300},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users: %v", err)
	}
	product := model.Product{
		SellerID: users[0].ID, Title: "测试教材", Description: "九成新", Price: 12.5,
		Category: "books", Condition: "九成新", Campus: "东校区",
		TradeLocation: "图书馆", Status: "on_sale",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("seed product: %v", err)
	}
	cfg := &config.Config{
		JWTSecret: "test-secret", JWTExpireHours: 24,
		RateLimitPerMin: 10000, LoginRateLimit: 10000,
		CORSOrigins: []string{"*"}, SeedingEnabled: false,
	}
	r := router.New(cfg, db, slog.Default())
	h := &harness{t: t, router: r}
	h.tokenA = h.login("13700000001", "123456")
	h.tokenB = h.login("13700000002", "123456")
	h.adminTo = h.login("13800000001", "admin123")
	return h
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (h *harness) login(phone, password string) string {
	body, _ := json.Marshal(map[string]string{"phone": phone, "password": password})
	rec := h.do(http.MethodPost, "/api/v1/users/login", "", body)
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != 0 {
		h.t.Fatalf("login %s failed: status=%d body=%s", phone, rec.Code, rec.Body.String())
	}
	var data struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if data.Token == "" {
		h.t.Fatalf("login %s returned empty token: %s", phone, rec.Body.String())
	}
	return data.Token
}

func (h *harness) do(method, path, token string, body []byte) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
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
