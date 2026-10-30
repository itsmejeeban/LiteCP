package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	qrcode "github.com/skip2/go-qrcode"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type twoFactorConfig struct {
	Enabled        bool      `json:"enabled"`
	SecretCipher   string    `json:"secretCipher,omitempty"`
	PendingCipher  string    `json:"pendingCipher,omitempty"`
	PendingUntil   time.Time `json:"pendingUntil,omitempty"`
	LastStep       int64     `json:"lastStep"`
	RecoveryHashes []string  `json:"recoveryHashes,omitempty"`
	Revision       string    `json:"revision,omitempty"`
}

func (s *Store) twoFactor() twoFactorConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value := s.cfg.Admin.TwoFactor
	value.RecoveryHashes = append([]string(nil), value.RecoveryHashes...)
	return value
}
func (s *Store) updateTwoFactor(update func(*twoFactorConfig) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.cfg.Admin.TwoFactor
	next := old
	next.RecoveryHashes = append([]string(nil), old.RecoveryHashes...)
	if err := update(&next); err != nil {
		return err
	}
	s.cfg.Admin.TwoFactor = next
	if err := s.saveLocked(); err != nil {
		s.cfg.Admin.TwoFactor = old
		return err
	}
	return nil
}
func totpCode(secret string, step int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(key) < 20 {
		return "", errors.New("invalid authenticator secret")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000), nil
}
func matchingTOTPStep(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	current := now.Unix() / 30
	matched := int64(-1)
	for step := current - 1; step <= current+1; step++ {
		expected, err := totpCode(secret, step)
		if err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 && step > last {
			matched = step
		}
	}
	return matched, matched >= 0
}
func recoveryHash(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}
func newRecoveryCodes() ([]string, []string, error) {
	codes, hashes := []string{}, []string{}
	for i := 0; i < 10; i++ {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, nil, err
		}
		code := hex.EncodeToString(raw[:])
		codes = append(codes, code)
		hashes = append(hashes, recoveryHash(code))
	}
	return codes, hashes, nil
}

var errAuthenticationCode = errors.New("authentication code was rejected")

func (a *App) verifySecondFactor(code string, allowRecovery bool) error {
	return a.store.updateTwoFactor(func(value *twoFactorConfig) error {
		if !value.Enabled {
			return nil
		}
		if allowRecovery {
			hash := recoveryHash(code)
			for i, expected := range value.RecoveryHashes {
				if subtle.ConstantTimeCompare([]byte(hash), []byte(expected)) == 1 {
					value.RecoveryHashes = append(value.RecoveryHashes[:i], value.RecoveryHashes[i+1:]...)
					return nil
				}
			}
		}
		// Recovery must remain usable even when the authenticator seed is unavailable.
		secret, err := a.system.DecryptSecret(value.SecretCipher)
		if err != nil {
			return errAuthenticationCode
		}
		if step, ok := matchingTOTPStep(secret, code, time.Now(), value.LastStep); ok {
			value.LastStep = step
			return nil
		}
		return errAuthenticationCode
	})
}
func (a *App) revokeAdministratorSessions() {
	a.mu.Lock()
	a.sessions = make(map[string]session)
	a.mu.Unlock()
}

func (a *App) twoFactorSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		state := a.store.twoFactor()
		writeJSON(w, 200, map[string]any{"enabled": state.Enabled, "recoveryCodesRemaining": len(state.RecoveryHashes)})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST")
		return
	}
	if !requestIsHTTPS(r) {
		writeError(w, 400, "Two-factor setup requires HTTPS.")
		return
	}
	var input struct {
		Action   string `json:"action"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if decodeJSONWithLimit(r, &input, 16<<10) != nil {
		writeError(w, 400, "Invalid authentication request.")
		return
	}
	if input.Action != "setup" && input.Action != "enable" && input.Action != "disable" && input.Action != "recovery" {
		writeError(w, 400, "Unknown two-factor action.")
		return
	}
	if !a.loginMu.TryLock() {
		writeError(w, 429, "Another authentication request is being checked.")
		return
	}
	defer a.loginMu.Unlock()
	if !a.reserveLoginAttempt(clientIP(r)) {
		writeError(w, 429, "Too many authentication attempts. Try again later.")
		return
	}
	if !a.store.CheckPassword(a.store.Username(), input.Password) {
		a.authenticationEvent("failed", r)
		writeError(w, 401, "Authentication failed.")
		return
	}
	state := a.store.twoFactor()
	if state.Enabled && (input.Action == "setup" || input.Action == "enable") {
		writeError(w, 409, "Two-factor authentication is already enabled.")
		return
	}
	if state.Enabled && a.verifySecondFactor(input.Code, true) != nil {
		a.authenticationEvent("failed", r)
		writeError(w, 401, "Enter a current authenticator code or an unused recovery code. A recovery code used to sign in cannot be used again.")
		return
	}
	if !state.Enabled && (input.Action == "disable" || input.Action == "recovery") {
		writeError(w, 409, "Two-factor authentication is not enabled.")
		return
	}
	if input.Action == "setup" {
		var raw [20]byte
		if _, err := rand.Read(raw[:]); err != nil {
			writeError(w, 500, "Could not create authenticator setup.")
			return
		}
		secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
		cipher, err := a.system.EncryptSecret(secret)
		if err != nil {
			writeError(w, 500, "Could not protect authenticator setup.")
			return
		}
		// Keep the label prefix and issuer identical for authenticator compatibility.
		issuer := "LiteCP@" + primaryIP()
		uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + issuer + ":" + a.store.Username()}
		query := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
		uri.RawQuery = query.Encode()
		png, err := qrcode.Encode(uri.String(), qrcode.Medium, 256)
		if err != nil {
			writeError(w, 500, "Could not create enrollment QR code.")
			return
		}
		err = a.store.updateTwoFactor(func(value *twoFactorConfig) error {
			value.PendingCipher = cipher
			value.PendingUntil = time.Now().Add(10 * time.Minute)
			return nil
		})
		if err != nil {
			writeError(w, 500, "Could not save authenticator setup.")
			return
		}
		a.clearLoginFailures(clientIP(r))
		writeJSON(w, 200, map[string]string{"secret": secret, "qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
		return
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		writeError(w, 500, "Could not generate recovery codes.")
		return
	}
	revision, err := randomToken(16)
	if err != nil {
		writeError(w, 500, "Could not secure authentication change.")
		return
	}
	err = a.store.updateTwoFactor(func(value *twoFactorConfig) error {
		if input.Action == "enable" {
			if !time.Now().Before(value.PendingUntil) || value.PendingCipher == "" {
				return errors.New("Authenticator setup expired. Start again.")
			}
			secret, err := a.system.DecryptSecret(value.PendingCipher)
			if err != nil {
				return errAuthenticationCode
			}
			step, ok := matchingTOTPStep(secret, input.Code, time.Now(), -1)
			if !ok {
				return errAuthenticationCode
			}
			value.Enabled = true
			value.SecretCipher = value.PendingCipher
			value.LastStep = step
			value.RecoveryHashes = hashes
		} else if input.Action == "disable" {
			*value = twoFactorConfig{}
		} else {
			value.RecoveryHashes = hashes
		}
		value.PendingCipher = ""
		value.PendingUntil = time.Time{}
		value.Revision = revision
		return nil
	})
	if err != nil {
		if errors.Is(err, errAuthenticationCode) {
			a.authenticationEvent("failed", r)
			writeError(w, 401, "Authentication failed.")
		} else {
			writeError(w, 400, "Could not complete the authenticator change. Setup may have expired.")
		}
		return
	}
	a.clearLoginFailures(clientIP(r))
	a.revokeAdministratorSessions()
	a.forgetBrowser(w, r)
	_ = a.store.AddActivity("Two-factor authentication changed", "Administrator action: "+input.Action, "warning")
	if input.Action == "disable" {
		codes = nil
	}
	writeJSON(w, 200, map[string]any{"ok": true, "signedOut": true, "recoveryCodes": codes})
}
