package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type APIRule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}
type APICredential struct {
	Identifier  string     `json:"identifier"`
	Description string     `json:"description"`
	AdminUser   string     `json:"adminUser"`
	RuleIDs     []string   `json:"ruleIds"`
	AllowedIPs  []string   `json:"allowedIPs"`
	SecretHash  string     `json:"secretHash,omitempty"`
	Enabled     bool       `json:"enabled"`
	RateLimit   int        `json:"rateLimit"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	LastAccess  *time.Time `json:"lastAccess,omitempty"`
	LastIP      string     `json:"lastIP,omitempty"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
}
type apiAction struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Unavailable string `json:"unavailable,omitempty"`
}

var apiActions = []apiAction{
	{"sites.list", "Websites", "List websites", ""}, {"sites.create", "Websites", "Create website", ""}, {"sites.suspend", "Websites", "Suspend website", ""}, {"sites.unsuspend", "Websites", "Unsuspend website", ""}, {"sites.delete", "Websites", "Permanently remove website data (backups retained)", ""}, {"sites.php", "Websites", "Change PHP version", ""}, {"sites.domain", "Websites", "Rename domain", ""}, {"sites.ssl", "Websites", "Issue or renew SSL", ""},
	{"databases.list", "Databases", "List managed databases", ""}, {"databases.create", "Databases", "Create database and local user", ""}, {"databases.password", "Databases", "Reset database password", ""}, {"databases.delete", "Databases", "Permanently delete database and user", ""},
	{"mailboxes.list", "Email", "List mailboxes (no password hashes)", ""}, {"mailboxes.create", "Email", "Create mailbox", ""}, {"mailboxes.password", "Email", "Reset mailbox password", ""}, {"mailboxes.delete", "Email", "Disable mailbox, retain messages", ""}, {"maildomains.list", "Email", "List mail domains", ""}, {"maildomains.create", "Email", "Add mail domain", ""},
	{"dns.read", "DNS", "Read website DNS zone", ""}, {"dns.write", "DNS", "Update website DNS zone", ""},
}

func knownAPIPermission(value string) bool {
	for _, p := range apiActions {
		if p.Name == value && p.Unavailable == "" {
			return true
		}
	}
	return false
}
func apiSecretHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
func containsAPIValue(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func validateAPIRule(rule APIRule) error {
	if strings.TrimSpace(rule.Name) == "" || len(rule.Name) > 100 || len(rule.Description) > 500 {
		return fmt.Errorf("Enter a rule name up to 100 characters and a description up to 500 characters")
	}
	if len(rule.Permissions) == 0 || len(rule.Permissions) > len(apiActions) {
		return fmt.Errorf("Select at least one API permission")
	}
	seen := map[string]bool{}
	for _, p := range rule.Permissions {
		if !knownAPIPermission(p) || seen[p] {
			return fmt.Errorf("Unknown or duplicate permission: %s", p)
		}
		seen[p] = true
	}
	return nil
}
func validateAPICredential(c APICredential, rules []APIRule) error {
	if strings.TrimSpace(c.Description) == "" || len(c.Description) > 200 {
		return fmt.Errorf("Enter a description up to 200 characters")
	}
	if c.RateLimit < 1 || c.RateLimit > 600 {
		return fmt.Errorf("Rate limit must be 1 to 600 requests per minute")
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("Expiry must be in the future")
	}
	if len(c.RuleIDs) == 0 || len(c.RuleIDs) > len(rules) {
		return fmt.Errorf("Assign at least one existing API rule")
	}
	seen := map[string]bool{}
	for _, id := range c.RuleIDs {
		found := false
		for _, r := range rules {
			if r.ID == id {
				found = true
			}
		}
		if !found || seen[id] {
			return fmt.Errorf("Unknown or duplicate API rule")
		}
		seen[id] = true
	}
	if len(c.AllowedIPs) > 50 {
		return fmt.Errorf("Use at most 50 IP addresses or CIDR networks")
	}
	for _, ip := range c.AllowedIPs {
		if net.ParseIP(ip) == nil {
			if _, _, err := net.ParseCIDR(ip); err != nil {
				return fmt.Errorf("Invalid allowed IP address or CIDR: %s", ip)
			}
		}
	}
	return nil
}
func apiIPAllowed(allowed []string, ip string) bool {
	if len(allowed) == 0 {
		return true
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, item := range allowed {
		if candidate := net.ParseIP(item); candidate != nil && candidate.Equal(parsed) {
			return true
		}
		if _, network, err := net.ParseCIDR(item); err == nil && network.Contains(parsed) {
			return true
		}
	}
	return false
}
func (a *App) apiAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.store.mu.RLock()
		rules := append([]APIRule{}, a.store.cfg.APIRules...)
		credentials := append([]APICredential{}, a.store.cfg.APICredentials...)
		admin := a.store.cfg.Admin.Username
		a.store.mu.RUnlock()
		for i := range credentials {
			credentials[i].SecretHash = ""
		}
		writeJSON(w, 200, map[string]any{"rules": rules, "credentials": credentials, "permissions": apiActions, "adminUser": admin})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "Use GET or POST")
		return
	}
	var input struct {
		Operation  string        `json:"operation"`
		Rule       APIRule       `json:"rule"`
		Credential APICredential `json:"credential"`
		ID         string        `json:"id"`
	}
	if err := decodeJSONWithLimit(r, &input, 32<<10); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	a.store.mu.Lock()
	oldRules := append([]APIRule{}, a.store.cfg.APIRules...)
	oldCredentials := append([]APICredential{}, a.store.cfg.APICredentials...)
	fail := func(code int, message string) {
		a.store.cfg.APIRules = oldRules
		a.store.cfg.APICredentials = oldCredentials
		a.store.mu.Unlock()
		writeError(w, code, message)
	}
	result := map[string]any{"ok": true}
	detail := ""
	switch input.Operation {
	case "save-rule":
		rule := input.Rule
		rule.Name = strings.TrimSpace(rule.Name)
		if err := validateAPIRule(rule); err != nil {
			fail(400, err.Error())
			return
		}
		for _, other := range oldRules {
			if strings.EqualFold(other.Name, rule.Name) && other.ID != rule.ID {
				fail(409, "An API rule with that name already exists")
				return
			}
		}
		if rule.ID == "" {
			if len(oldRules) >= 100 {
				fail(400, "At most 100 API rules are supported")
				return
			}
			id, err := randomToken(16)
			if err != nil {
				fail(500, "Cannot generate rule identifier")
				return
			}
			rule.ID = id
			a.store.cfg.APIRules = append(a.store.cfg.APIRules, rule)
		} else {
			found := false
			for i, other := range a.store.cfg.APIRules {
				if other.ID == rule.ID {
					a.store.cfg.APIRules[i] = rule
					found = true
				}
			}
			if !found {
				fail(404, "API rule not found")
				return
			}
		}
		detail = rule.Name
	case "remove-rule":
		for _, c := range oldCredentials {
			if containsAPIValue(c.RuleIDs, input.ID) {
				fail(409, "This rule is assigned to a credential. Edit its assignments first.")
				return
			}
		}
		found := false
		next := []APIRule{}
		for _, rule := range oldRules {
			if rule.ID == input.ID {
				found = true
				detail = rule.Name
			} else {
				next = append(next, rule)
			}
		}
		if !found {
			fail(404, "API rule not found")
			return
		}
		a.store.cfg.APIRules = next
	case "create-credential", "save-credential":
		c := input.Credential
		c.Description = strings.TrimSpace(c.Description)
		if c.RateLimit == 0 {
			c.RateLimit = 60
		}
		if err := validateAPICredential(c, oldRules); err != nil {
			fail(400, err.Error())
			return
		}
		if input.Operation == "create-credential" {
			if len(oldCredentials) >= 200 {
				fail(400, "At most 200 credentials are supported")
				return
			}
			id, err := randomToken(24)
			if err != nil {
				fail(500, "Cannot generate identifier")
				return
			}
			secret, err := randomToken(48)
			if err != nil {
				fail(500, "Cannot generate secret")
				return
			}
			c.Identifier = id
			c.SecretHash = apiSecretHash(secret)
			c.CreatedAt = time.Now().UTC()
			c.AdminUser = a.store.cfg.Admin.Username
			c.LastAccess = nil
			c.LastIP = ""
			c.RevokedAt = nil
			a.store.cfg.APICredentials = append(a.store.cfg.APICredentials, c)
			result["identifier"] = id
			result["secret"] = secret
		} else {
			found := false
			for i, old := range oldCredentials {
				if old.Identifier == c.Identifier {
					if old.RevokedAt != nil && c.Enabled {
						fail(400, "Revoked credentials cannot be re-enabled. Generate a new credential.")
						return
					}
					c.RevokedAt = old.RevokedAt
					c.SecretHash = old.SecretHash
					c.CreatedAt = old.CreatedAt
					c.AdminUser = old.AdminUser
					c.LastAccess = old.LastAccess
					c.LastIP = old.LastIP
					a.store.cfg.APICredentials[i] = c
					found = true
				}
			}
			if !found {
				fail(404, "API credential not found")
				return
			}
		}
		detail = c.Description
	case "revoke-credential":
		found := false
		for i, c := range oldCredentials {
			if c.Identifier == input.ID {
				a.store.cfg.APICredentials[i].Enabled = false
				now := time.Now().UTC()
				a.store.cfg.APICredentials[i].RevokedAt = &now
				found = true
				detail = c.Description
			}
		}
		if !found {
			fail(404, "API credential not found")
			return
		}
	default:
		fail(400, "Unknown API management operation")
		return
	}
	if err := a.store.saveLocked(); err != nil {
		fail(500, "API configuration could not be saved")
		return
	}
	a.store.mu.Unlock()
	a.store.AddActivity("API access changed", input.Operation+": "+detail, "info")
	writeJSON(w, 200, result)
}

// In-memory bounded windows limit invalid authentication as well as each key.
func (a *App) apiRateAllowed(key string, limit int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.apiBuckets == nil {
		a.apiBuckets = map[string]loginAttempt{}
	}
	now := time.Now()
	if len(a.apiBuckets) >= 4096 {
		for k, b := range a.apiBuckets {
			if now.Sub(b.Since) >= time.Minute {
				delete(a.apiBuckets, k)
			}
		}
		if len(a.apiBuckets) >= 4096 {
			if _, ok := a.apiBuckets[key]; !ok {
				return false
			}
		}
	}
	bucket := a.apiBuckets[key]
	if now.Sub(bucket.Since) >= time.Minute {
		bucket = loginAttempt{Since: now}
	}
	if bucket.Count >= limit {
		return false
	}
	bucket.Count++
	a.apiBuckets[key] = bucket
	return true
}

// Reject invalid credentials before reading an untrusted body. Authentication is
// checked again under the mutation lock after parsing to preserve revocation.
func (a *App) validAPICredentialsBeforeBody(id, secret, ip string) bool {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	var credential *APICredential
	for i := range a.store.cfg.APICredentials {
		if a.store.cfg.APICredentials[i].Identifier == id {
			credential = &a.store.cfg.APICredentials[i]
			break
		}
	}
	expected := strings.Repeat("0", 64)
	if credential != nil {
		expected = credential.SecretHash
	}
	valid := subtle.ConstantTimeCompare([]byte(apiSecretHash(secret)), []byte(expected)) == 1
	return valid && credential != nil && credential.Enabled && credential.RevokedAt == nil &&
		(credential.ExpiresAt == nil || credential.ExpiresAt.After(time.Now())) && apiIPAllowed(credential.AllowedIPs, ip)
}

func (a *App) externalAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !requestIsHTTPS(r) {
		writeError(w, 403, "External API requires HTTPS")
		return
	}
	// Never accept browser sessions as external API authorization.
	ip := clientIP(r)
	if !a.apiRateAllowed("ip:"+ip, 600) {
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "API request limit exceeded")
		return
	}
	rawMode := r.URL.Path == "/api/v1/request"
	if !rawMode && r.URL.RawQuery != "" {
		writeError(w, 400, "Use headers for credentials and a JSON request body")
		return
	}
	id, secret := r.Header.Get("X-LiteCP-Identifier"), r.Header.Get("X-LiteCP-Secret")
	if len(id) > 128 || len(secret) > 256 || id == "" || secret == "" {
		writeError(w, 401, "Invalid API credentials")
		return
	}
	if !a.validAPICredentialsBeforeBody(id, secret, ip) {
		writeError(w, 401, "Invalid API credentials or source address")
		return
	}
	var input struct {
		Action     string          `json:"action"`
		Parameters json.RawMessage `json:"parameters"`
		ObjectID   string          `json:"objectId"`
		Query      url.Values      `json:"query"`
	}
	if rawMode {
		input.Action = r.Header.Get("X-LiteCP-Action")
		input.ObjectID = r.Header.Get("X-LiteCP-Object-ID")
		input.Query = r.URL.Query()
		if _, ok := apiRouteActions[input.Action]; !ok {
			writeError(w, 400, "Streaming requests require an explicit panel action from the catalog")
			return
		}
	} else if err := decodeJSONWithLimit(r, &input, 64<<10); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// Reads and interactive terminal traffic must not wait behind long uploads
	// or hold the global mutation lock while a client polls output.
	mutating := true
	if route, ok := apiRouteActions[input.Action]; ok {
		mutating = route.Method != http.MethodGet && route.Path != "/api/terminal/session" && route.Path != "/api/live-migrations/cancel" && route.Path != "/api/files/upload-url"
	}
	if mutating {
		a.mutationMu.Lock()
		defer a.mutationMu.Unlock()
	}
	a.store.mu.Lock()
	var credential *APICredential
	for i := range a.store.cfg.APICredentials {
		if a.store.cfg.APICredentials[i].Identifier == id {
			credential = &a.store.cfg.APICredentials[i]
			break
		}
	}
	hash := apiSecretHash(secret)
	valid := credential != nil
	expected := strings.Repeat("0", 64)
	if valid {
		expected = credential.SecretHash
	}
	valid = subtle.ConstantTimeCompare([]byte(hash), []byte(expected)) == 1 && valid
	if !valid || !credential.Enabled || credential.RevokedAt != nil || (credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now())) || !apiIPAllowed(credential.AllowedIPs, ip) {
		a.store.mu.Unlock()
		writeError(w, 401, "Invalid API credentials or source address")
		return
	}
	allowed := false
	for _, rule := range a.store.cfg.APIRules {
		if containsAPIValue(credential.RuleIDs, rule.ID) && containsAPIValue(rule.Permissions, input.Action) {
			allowed = true
		}
	}
	if !allowed || !knownAPIPermission(input.Action) {
		a.store.mu.Unlock()
		writeError(w, 403, "API rule does not allow this action")
		return
	}
	if !a.apiRateAllowed("credential:"+id, credential.RateLimit) {
		a.store.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		writeError(w, 429, "Credential request limit exceeded")
		return
	}
	old := *credential
	now := time.Now().UTC()
	credential.LastAccess = &now
	credential.LastIP = ip
	if err := a.store.saveLocked(); err != nil {
		*credential = old
		a.store.mu.Unlock()
		writeError(w, 500, "Cannot record API access")
		return
	}
	a.store.mu.Unlock()
	if _, ok := apiRouteActions[input.Action]; ok {
		if !rawMode {
			if len(input.Parameters) == 0 {
				input.Parameters = json.RawMessage(`{}`)
			}
			var object map[string]json.RawMessage
			if json.Unmarshal(input.Parameters, &object) != nil || object == nil {
				writeError(w, 400, "parameters must be a JSON object")
				return
			}
		}
		a.store.AddActivity("External API action", input.Action+" via "+id+" from "+ip, "info")
		a.dispatchAPIRoute(w, r, input.Action, input.ObjectID, input.Query, input.Parameters, rawMode)
		return
	}
	if len(input.Parameters) == 0 {
		input.Parameters = json.RawMessage(`{}`)
	}
	var parameters map[string]json.RawMessage
	if err := json.Unmarshal(input.Parameters, &parameters); err != nil || parameters == nil {
		writeError(w, 400, "parameters must be a JSON object")
		return
	}
	request := r.Clone(r.Context())
	request.Header = make(http.Header)
	request.Header.Set("Content-Type", "application/json")
	request.URL = &url.URL{}
	request.Method = http.MethodPost
	value := func(key string) string { var value string; _ = json.Unmarshal(parameters[key], &value); return value }
	body := input.Parameters
	setBody := func() {
		request.Body = http.NoBody
		if len(body) > 0 {
			request.Body = ioNopAPI(body)
		}
		request.ContentLength = int64(len(body))
	}
	var handler http.HandlerFunc
	switch input.Action {
	case "sites.list":
		writeJSON(w, 200, map[string]any{"items": a.store.Sites()})
		return
	case "databases.list":
		a.store.mu.RLock()
		items := append([]ManagedDatabase{}, a.store.cfg.Databases...)
		a.store.mu.RUnlock()
		writeJSON(w, 200, map[string]any{"items": items})
		return
	case "maildomains.list", "mailboxes.list":
		domains, boxes, _ := a.store.MailConfig()
		for i := range boxes {
			boxes[i].PasswordHash = ""
		}
		if input.Action == "mailboxes.list" {
			writeJSON(w, 200, map[string]any{"items": boxes})
		} else {
			writeJSON(w, 200, map[string]any{"items": domains})
		}
		return
	case "sites.create":
		handler = a.createSite
	case "sites.suspend", "sites.unsuspend":
		parameters["suspended"] = json.RawMessage("false")
		if input.Action == "sites.suspend" {
			parameters["suspended"] = json.RawMessage("true")
		}
		body, _ = json.Marshal(parameters)
		handler = a.siteSuspension
	case "sites.php":
		handler = a.changeSitePHP
	case "sites.domain":
		request.Method = http.MethodPut
		handler = a.changeSiteDomain
	case "sites.ssl":
		handler = a.enableSSL
	case "sites.delete":
		if value("id") == "" || strings.ContainsAny(value("id"), "/\\?#") {
			writeError(w, 400, "A valid website id and exact domain are required")
			return
		}
		request.Method = http.MethodDelete
		request.URL.Path = "/api/sites/" + value("id")
		request.URL.RawQuery = "deleteData=true"
		// The existing delete handler accepts only its domain confirmation body.
		body, _ = json.Marshal(map[string]string{"domain": value("domain")})
		handler = a.deleteSite
	case "databases.create":
		handler = a.createManagedDatabase
	case "databases.password":
		request.Method = http.MethodPut
		handler = a.resetManagedDatabasePassword
	case "databases.delete", "mailboxes.delete":
		id := value("id")
		if id == "" || strings.ContainsAny(id, "/\\?#") {
			writeError(w, 400, "A valid object id is required")
			return
		}
		request.Method = http.MethodDelete
		if input.Action == "databases.delete" {
			request.URL.Path = "/api/databases/" + id
			handler = a.deleteManagedDatabase
		} else {
			request.URL.Path = "/api/mail/mailboxes/" + id
			handler = a.deleteMailbox
		}
	case "maildomains.create":
		handler = a.createMailDomain
	case "mailboxes.create":
		handler = a.createMailbox
	case "mailboxes.password":
		request.Method = http.MethodPut
		handler = a.resetMailboxPassword
	case "dns.read", "dns.write":
		request.URL.RawQuery = url.Values{"site": {value("site")}}.Encode()
		request.Method = http.MethodGet
		if input.Action == "dns.write" {
			request.Method = http.MethodPut
		}
		handler = a.websiteDNS
	}
	if handler == nil {
		writeError(w, 400, "Unknown API action")
		return
	}
	setBody()
	a.store.AddActivity("External API action", input.Action+" via "+id+" from "+ip, "info")
	handler(w, request)
}

// A fresh body prevents credentials or outer envelope fields reaching handlers.
func ioNopAPI(body []byte) *apiBody { return &apiBody{bytes.NewReader(body)} }

type apiBody struct{ *bytes.Reader }

func (*apiBody) Close() error { return nil }
