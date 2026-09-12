// Package testsupport wires the real application router against any GORM
// database for tests, and seeds the three default accounts plus one product.
// Integration tests use it with an in-memory MySQL engine; the MySQL
// regression tests use it with a real MariaDB/InnoDB server.
package testsupport

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lp/campus-market/internal/config"
	"github.com/lp/campus-market/internal/database"
	"github.com/lp/campus-market/internal/model"
	"github.com/lp/campus-market/internal/router"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// App is a fully wired application for tests.
type App struct {
	t       *testing.T
	DB      *gorm.DB
	Handler http.Handler
	TokenA  string // student 13700000001
	TokenB  string // student 13700000002
	TokenC  string // admin 13800000001
}

// MigrateAndSeed migrates the schema and seeds the standard demo rows.
func MigrateAndSeed(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	seed(t, db)
}

func seed(t *testing.T, db *gorm.DB) {
	t.Helper()
	mustHash := func(p string) string {
		h, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt: %v", err)
		}
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
}

// New builds the real router over db (no seeding; call MigrateAndSeed first
// when starting from an empty schema).
func New(t *testing.T, db *gorm.DB) *App {
	t.Helper()
	cfg := &config.Config{
		JWTSecret: "test-secret", JWTExpireHours: 24,
		RateLimitPerMin: 10000, LoginRateLimit: 10000,
		CORSOrigins: []string{"*"}, SeedingEnabled: false,
	}
	app := &App{t: t, DB: db, Handler: router.New(cfg, db, slog.Default())}
	app.TokenA = app.Login("13700000001", "123456")
	app.TokenB = app.Login("13700000002", "123456")
	app.TokenC = app.Login("13800000001", "admin123")
	return app
}

// Envelope mirrors the unified JSON response.
type Envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Login exchanges phone/password for a JWT through the real endpoint.
func (a *App) Login(phone, password string) string {
	body, _ := json.Marshal(map[string]string{"phone": phone, "password": password})
	rec := a.Do(http.MethodPost, "/api/v1/users/login", "", body)
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Code != 0 {
		a.t.Fatalf("login %s failed: status=%d body=%s", phone, rec.Code, rec.Body.String())
	}
	var data struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(env.Data, &data)
	if data.Token == "" {
		a.t.Fatalf("login %s returned empty token", phone)
	}
	return data.Token
}

// Do performs an HTTP request against the in-process router.
func (a *App) Do(method, path, token string, body []byte) *httptest.ResponseRecorder {
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
	a.Handler.ServeHTTP(rec, req)
	return rec
}

// MustData decodes a successful response or fails the test.
func (a *App) MustData(rec *httptest.ResponseRecorder, where string) json.RawMessage {
	a.t.Helper()
	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		a.t.Fatalf("%s: bad json: %v body=%s", where, err, rec.Body.String())
	}
	if env.Code != 0 {
		a.t.Fatalf("%s: code=%d message=%s", where, env.Code, env.Message)
	}
	return env.Data
}

// JSON is a shorthand payload builder.
func JSON(v map[string]any) []byte {
	b, _ := json.Marshal(v)
	return b
}
