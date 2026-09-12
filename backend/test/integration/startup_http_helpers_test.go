package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// jsonUnmarshalData extracts the `data` field of the unified envelope.
func jsonUnmarshalData(raw []byte, target any) error {
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	return json.Unmarshal(env.Data, target)
}

// login obtains a JWT through the live HTTP server.
func login(t *testing.T, base, phone, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"phone":%q,"password":%q}`, phone, password)
	resp, err := http.Post(base+"/api/v1/users/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s status=%d body=%s", phone, resp.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || env.Data.Token == "" {
		t.Fatalf("login decode: %s", raw)
	}
	return env.Data.Token
}
