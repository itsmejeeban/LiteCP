package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTOTPStandardVectorsAndStrictCodes(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for _, v := range []struct {
		seconds int64
		code    string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		code, err := totpCode(secret, v.seconds/30)
		if err != nil || code != v.code {
			t.Fatalf("RFC vector %d got %s %v", v.seconds, code, err)
		}
	}
	for _, code := range []string{"28708", "0287082", "abcdef", "28708 ", ""} {
		if _, ok := matchingTOTPStep(secret, code, time.Unix(59, 0), -1); ok {
			t.Fatal("accepted malformed code")
		}
	}
	if _, ok := matchingTOTPStep(secret, "287082", time.Unix(59, 0), 1); ok {
		t.Fatal("accepted replay")
	}
	if _, ok := matchingTOTPStep(secret, "287082", time.Unix(180, 0), -1); ok {
		t.Fatal("accepted expired code")
	}
}

func twoFactorTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Initialize("admin", "test-password-long-enough"); err != nil {
		t.Fatal(err)
	}
	return &App{store: store, system: NewSystem(dir), sessions: map[string]session{}, attempts: map[string]loginAttempt{}}
}
func factorRequest(app *App, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "https://panel.test/api/security/two-factor", strings.NewReader(body))
	req.RemoteAddr = "192.0.2.5:1000"
	response := httptest.NewRecorder()
	app.twoFactorSettings(response, req)
	return response
}

func TestTwoFactorEnrollmentRecoveryAndPersistence(t *testing.T) {
	app := twoFactorTestApp(t)
	app.rememberSession("old-session", time.Now().Add(time.Hour))
	response := factorRequest(app, `{"action":"setup","password":"test-password-long-enough"}`)
	if response.Code != 200 {
		t.Fatalf("setup: %d %s", response.Code, response.Body.String())
	}
	var setup map[string]string
	json.Unmarshal(response.Body.Bytes(), &setup)
	if !strings.HasPrefix(setup["qr"], "data:image/png;base64,") || len(setup["secret"]) != 32 {
		t.Fatal("missing local QR or secret")
	}
	disk, _ := os.ReadFile(app.store.path)
	if bytes.Contains(disk, []byte(setup["secret"])) {
		t.Fatal("plaintext seed stored")
	}
	if app.store.twoFactor().Enabled {
		t.Fatal("unverified setup activated")
	}
	code, _ := totpCode(setup["secret"], time.Now().Unix()/30)
	response = factorRequest(app, fmtFactorBody("enable", code))
	if response.Code != 200 {
		t.Fatalf("enable: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	json.Unmarshal(response.Body.Bytes(), &result)
	if len(result.RecoveryCodes) != 10 || app.validSession("old-session") {
		t.Fatal("recovery codes or session revocation missing")
	}
	disk, _ = os.ReadFile(app.store.path)
	for _, code := range result.RecoveryCodes {
		if bytes.Contains(disk, []byte(code)) {
			t.Fatal("plaintext recovery code stored")
		}
	}
	if err := app.verifySecondFactor(code, true); err == nil {
		t.Fatal("activation code replayed")
	}
	recovery := result.RecoveryCodes[0]
	if err := app.verifySecondFactor(recovery, true); err != nil {
		t.Fatal(err)
	}
	if err := app.verifySecondFactor(recovery, true); err == nil {
		t.Fatal("recovery reused")
	}
	reopened, err := OpenStore(filepath.Dir(app.store.path))
	if err != nil {
		t.Fatal(err)
	}
	app.store = reopened
	if err := app.verifySecondFactor(recovery, true); err == nil {
		t.Fatal("recovery reuse after restart")
	}
	if err := app.store.SetPassword("replacement-password-long"); err != nil {
		t.Fatal(err)
	}
	if !app.store.twoFactor().Enabled {
		t.Fatal("password change bypassed 2FA")
	}
}
func fmtFactorBody(action, code string) string {
	data, _ := json.Marshal(map[string]string{"action": action, "password": "test-password-long-enough", "code": code})
	return string(data)
}

func TestLoginRequiresSecondFactorAndRejectsRecoveryReplay(t *testing.T) {
	app := twoFactorTestApp(t)
	cipher, err := app.system.EncryptSecret("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if err != nil {
		t.Fatal(err)
	}
	if err = app.store.updateTwoFactor(func(state *twoFactorConfig) error {
		state.Enabled = true
		state.SecretCipher = cipher
		state.RecoveryHashes = []string{recoveryHash("login-recovery")}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		code   string
		status int
	}{{"", 401}, {"invalid", 401}, {"login-recovery", 200}, {"login-recovery", 401}} {
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": "test-password-long-enough", "code": attempt.code})
		req := httptest.NewRequest(http.MethodPost, "https://panel.test/api/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.15:1234"
		response := httptest.NewRecorder()
		app.login(response, req)
		if response.Code != attempt.status {
			t.Fatalf("login status %d want %d: %s", response.Code, attempt.status, response.Body.String())
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Value != "" && attempt.status != 200 {
				t.Fatal("failed login issued session")
			}
		}
	}
}

func TestRecoveryConsumptionIsAtomicAndFailureSafe(t *testing.T) {
	app := twoFactorTestApp(t)
	cipher, err := app.system.EncryptSecret("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	if err != nil {
		t.Fatal(err)
	}
	err = app.store.updateTwoFactor(func(state *twoFactorConfig) error {
		state.Enabled = true
		state.SecretCipher = cipher
		state.RecoveryHashes = []string{recoveryHash("random-recovery")}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	successes := make(chan bool, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); successes <- app.verifySecondFactor("random-recovery", true) == nil }()
	}
	wg.Wait()
	close(successes)
	count := 0
	for value := range successes {
		if value {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("accepted concurrent recovery %d times", count)
	}
	app.store.mu.Lock()
	app.store.cfg.Admin.TwoFactor.RecoveryHashes = []string{recoveryHash("second-recovery")}
	app.store.path = filepath.Join(app.store.path, "missing", "config.json")
	app.store.mu.Unlock()
	if app.verifySecondFactor("second-recovery", true) == nil {
		t.Fatal("success despite persistence failure")
	}
	if len(app.store.twoFactor().RecoveryHashes) != 1 {
		t.Fatal("failed write consumed recovery code")
	}
}

func TestMFARejectsTrustedBrowserBypass(t *testing.T) {
	app := twoFactorTestApp(t)
	token := "trusted-test-token"
	state := developerSettings{Enabled: true, Browsers: map[string]trustedBrowser{trustHash(token): {Expires: time.Now().Add(time.Hour), Credential: app.credentialStamp()}}}
	if err := app.developerWrite(state); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://panel.test/api/state", nil)
	req.AddCookie(&http.Cookie{Name: "litecp_trusted", Value: token})
	if !app.trustedRequest(req) {
		t.Fatal("fixture not trusted")
	}
	app.store.cfg.Admin.TwoFactor.Enabled = true
	if app.trustedRequest(req) {
		t.Fatal("trusted browser bypassed 2FA")
	}
}
