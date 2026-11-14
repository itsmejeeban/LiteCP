package main

import (
	"crypto/rand"
	"encoding/base32"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type operatorContextKey struct{}

type PanelOperator struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Role         string    `json:"role"`
	Enabled      bool      `json:"enabled"`
	Revision     string    `json:"revision"`
	CreatedAt    time.Time `json:"createdAt"`
	PasswordHash string    `json:"passwordHash,omitempty"`
	TOTPCipher   string    `json:"totpCipher,omitempty"`
	LastStep     int64     `json:"lastStep"`
}

func validOperatorRole(role string) bool {
	return role == "viewer" || role == "websites" || role == "email" || role == "backups"
}
func operatorRouteAllowed(role, method, path string) bool {
	read := method == "GET" || method == "HEAD"
	if read && (path == "/api/state" || path == "/api/version" || path == "/api/overview/metrics") || path == "/api/logout" && method == "POST" {
		return true
	}
	if role == "viewer" {
		return read && (path == "/api/overview/jobs" || path == "/api/analytics/usage")
	}
	if role == "websites" {
		allowed := map[string]string{"/api/sites": "POST", "/api/sites/suspension": "PUT", "/api/sites/php": "POST", "/api/sites/php-settings": "GET,PUT", "/api/sites/ssl": "POST", "/api/sites/sizes": "GET", "/api/sites/database-sizes": "GET", "/api/sites/traffic": "GET", "/api/website-cron": "GET,POST,PUT,DELETE", "/api/sites/dns": "GET,PUT", "/api/sites/redis": "GET,POST,PUT"}
		return containsAPIValue(strings.Split(allowed[path], ","), method)
	}
	if role == "email" {
		if strings.HasPrefix(path, "/api/mail/mailboxes/") {
			return method == "DELETE" || path == "/api/mail/mailboxes/password" && method == "PUT"
		}
		if strings.HasPrefix(path, "/api/mail/domains/") {
			return method == "DELETE"
		}
		if strings.HasPrefix(path, "/api/mail/aliases/") {
			return method == "PUT" || method == "DELETE"
		}
		allowed := map[string]string{"/api/mail/domains": "GET,POST", "/api/mail/mailboxes": "GET,POST", "/api/mail/aliases": "GET,POST", "/api/mail/delivery": "GET"}
		return containsAPIValue(strings.Split(allowed[path], ","), method)
	}
	if role == "backups" {
		return path == "/api/backups" && method == "POST" || read && (path == "/api/backups/counts" || path == "/api/backups/restorable" || path == "/api/overview/jobs")
	}
	return false
}
func (a *App) operatorSession(r *http.Request) (*PanelOperator, bool) {
	if a.store == nil {
		return nil, false
	}
	cookie, err := r.Cookie("litecp_session")
	if err != nil {
		return nil, false
	}
	a.mu.Lock()
	session, ok := a.sessions[cookie.Value]
	a.mu.Unlock()
	if !ok || session.OperatorID == "" {
		return nil, false
	}
	if !session.Expires.After(time.Now()) {
		return nil, true
	}
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	for _, operator := range a.store.cfg.Operators {
		if operator.ID == session.OperatorID && operator.Enabled && operator.Revision == session.OperatorRevision {
			return &operator, true
		}
	}
	return nil, true
}

func (a *App) requestOperator(r *http.Request) *PanelOperator {
	if operator, ok := r.Context().Value(operatorContextKey{}).(*PanelOperator); ok {
		return operator
	}
	operator, _ := a.operatorSession(r)
	return operator
}

func (a *App) requestActor(r *http.Request) string {
	if op := a.requestOperator(r); op != nil {
		return op.Username
	}
	if validExternalAPIGrant(r) {
		return "external-api"
	}
	return a.store.Username()
}
func (a *App) authenticateOperator(username, password, code string) (*PanelOperator, bool) {
	a.store.mu.RLock()
	var selected *PanelOperator
	for _, v := range a.store.cfg.Operators {
		if v.Username == username && v.Enabled {
			copy := v
			selected = &copy
			break
		}
	}
	a.store.mu.RUnlock()
	if selected == nil {
		_ = a.store.CheckPassword(username, password)
		return nil, false
	}
	if !verifyPassword(selected.PasswordHash, password) {
		return nil, false
	}
	if strings.TrimSpace(code) == "" {
		return selected, false
	}
	secret, e := a.system.DecryptSecret(selected.TOTPCipher)
	if e != nil {
		return nil, false
	}
	step, ok := matchingTOTPStep(secret, strings.TrimSpace(code), time.Now(), selected.LastStep)
	if !ok {
		return nil, false
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	for i := range a.store.cfg.Operators {
		v := &a.store.cfg.Operators[i]
		if v.ID == selected.ID && v.Enabled && v.Revision == selected.Revision && step > v.LastStep {
			old := v.LastStep
			v.LastStep = step
			if a.store.saveLocked() != nil {
				v.LastStep = old
				return nil, false
			}
			return selected, true
		}
	}
	return nil, false
}
func (a *App) operatorsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.store.mu.RLock()
		items := append([]PanelOperator{}, a.store.cfg.Operators...)
		a.store.mu.RUnlock()
		for i := range items {
			items[i].PasswordHash = ""
			items[i].TOTPCipher = ""
			items[i].LastStep = 0
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	if r.Method != "POST" {
		writeError(w, 405, "Use GET or POST")
		return
	}
	var input struct {
		Action, Username, Role, ID string
		Enabled                    bool
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "Invalid operator request")
		return
	}
	if input.Action == "create" {
		if !validOperatorRole(input.Role) || len(input.Username) < 3 || len(input.Username) > 48 || input.Username == a.store.Username() {
			writeError(w, 400, "Choose a unique username (3–48 characters) and a limited role")
			return
		}
		for _, c := range input.Username {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				writeError(w, 400, "Use letters, digits, underscore or hyphen")
				return
			}
		}
		password, e := randomToken(24)
		if e != nil {
			writeError(w, 500, "Credential generation failed")
			return
		}
		hash, e := hashPassword(password)
		if e != nil {
			writeError(w, 500, "Password hashing failed")
			return
		}
		var raw [20]byte
		if _, e = rand.Read(raw[:]); e != nil {
			writeError(w, 500, "Authenticator generation failed")
			return
		}
		secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
		cipher, e := a.system.EncryptSecret(secret)
		if e != nil {
			writeError(w, 500, "Cannot protect authenticator seed")
			return
		}
		id, e := randomToken(16)
		if e != nil {
			writeError(w, 500, "Account identifier generation failed")
			return
		}
		item := PanelOperator{ID: id, Username: input.Username, Role: input.Role, Enabled: true, Revision: id, CreatedAt: time.Now().UTC(), PasswordHash: hash, TOTPCipher: cipher, LastStep: -1}
		a.store.mu.Lock()
		for _, v := range a.store.cfg.Operators {
			if v.Username == input.Username {
				a.store.mu.Unlock()
				writeError(w, 409, "Username already exists")
				return
			}
		}
		if len(a.store.cfg.Operators) >= 100 {
			a.store.mu.Unlock()
			writeError(w, 409, "Operator limit reached")
			return
		}
		old := a.store.cfg.Operators
		a.store.cfg.Operators = append(append([]PanelOperator{}, old...), item)
		e = a.store.saveLocked()
		if e != nil {
			a.store.cfg.Operators = old
		}
		a.store.mu.Unlock()
		if e != nil {
			writeError(w, 500, "Cannot save operator account")
			return
		}
		uri := url.URL{Scheme: "otpauth", Host: "totp", Path: "/LiteCP:" + item.Username}
		q := url.Values{"secret": {secret}, "issuer": {"LiteCP"}, "digits": {"6"}, "period": {"30"}}
		uri.RawQuery = q.Encode()
		writeJSON(w, 201, map[string]string{"username": input.Username, "password": password, "authenticatorSecret": secret, "authenticatorURI": uri.String(), "message": "Save these once-only credentials privately. A unique authenticator code is required at every login."})
		return
	}
	if input.Action != "update" || !validOperatorRole(input.Role) {
		writeError(w, 400, "Unknown operator action or role")
		return
	}
	revision, e := randomToken(16)
	if e != nil {
		writeError(w, 500, "Cannot revise account")
		return
	}
	a.store.mu.Lock()
	found := false
	original := append([]PanelOperator{}, a.store.cfg.Operators...)
	for i := range a.store.cfg.Operators {
		v := &a.store.cfg.Operators[i]
		if v.ID == input.ID {
			v.Role = input.Role
			v.Enabled = input.Enabled
			v.Revision = revision
			found = true
		}
	}
	if !found {
		a.store.mu.Unlock()
		writeError(w, 404, "Operator not found")
		return
	}
	e = a.store.saveLocked()
	if e != nil {
		a.store.cfg.Operators = original
	}
	a.store.mu.Unlock()
	if e != nil {
		writeError(w, 500, "Cannot save operator state")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func redactOperatorState(state *PublicState, op PanelOperator) {
	state.Username = op.Username
	state.AccessRole = op.Role
	state.Settings = Settings{Timezone: state.Settings.Timezone}
	state.Activities = []Activity{}
	state.BackupDestinations = []BackupDestination{}
	state.FirewallRules = []FirewallRule{}
	if op.Role != "websites" && op.Role != "backups" {
		state.Sites = []Site{}
	}
	if op.Role != "email" {
		state.Mailboxes = []Mailbox{}
		state.MailDomains = []MailDomain{}
		state.MailAliases = []MailAlias{}
	}
	if op.Role != "backups" {
		state.Backups = []Backup{}
	}
	state.Databases = []ManagedDatabase{}
	for i := range state.Sites {
		state.Sites[i].Database = ""
		state.Sites[i].DBUser = ""
	}
}
