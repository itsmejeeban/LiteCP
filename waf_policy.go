package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Policies are compiled, never interpolated as administrator-supplied Nginx directives.
type WAFCondition struct {
	Field    string         `json:"field,omitempty"`
	Key      string         `json:"key,omitempty"`
	Operator string         `json:"operator,omitempty"`
	Value    string         `json:"value,omitempty"`
	Join     string         `json:"join,omitempty"`
	Children []WAFCondition `json:"children,omitempty"`
}
type WAFCustomRule struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Enabled bool         `json:"enabled"`
	Action  string       `json:"action"`
	Match   WAFCondition `json:"match"`
}
type WAFRateLimit struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	PathPrefix string `json:"pathPrefix"`
	Requests   int    `json:"requests"`
	Burst      int    `json:"burst"`
}
type WAFPolicy struct {
	Mode       string          `json:"mode"`
	Managed    bool            `json:"managed"`
	Rules      []WAFCustomRule `json:"rules"`
	RateLimits []WAFRateLimit  `json:"rateLimits"`
}
type WAFPolicies struct {
	Server   WAFPolicy            `json:"server"`
	Websites map[string]WAFPolicy `json:"websites"`
}

const wafPolicyPath = "/etc/litecp/waf/policies.json"
const wafGlobalPath = "/etc/litecp/waf/global-policy.conf"
const wafHTTPPath = "/etc/nginx/conf.d/00-litecp-waf-policy.conf"

var wafIdentifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)
var wafHeaderKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func defaultWAFPolicy() WAFPolicy {
	return WAFPolicy{Mode: "off", Rules: []WAFCustomRule{}, RateLimits: []WAFRateLimit{}}
}
func readWAFPolicies(path string) (WAFPolicies, error) {
	p := WAFPolicies{Server: defaultWAFPolicy(), Websites: map[string]WAFPolicy{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if err := validateWAFPolicy(p.Server); err != nil {
		return p, err
	}
	for id, policy := range p.Websites {
		if !wafIdentifier.MatchString(id) {
			return p, errors.New("invalid stored WAF scope")
		}
		if err := validateWAFPolicy(policy); err != nil {
			return p, err
		}
	}
	if p.Websites == nil {
		p.Websites = map[string]WAFPolicy{}
	}
	return p, nil
}
func wafQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
func wafVariable(c WAFCondition) (string, error) {
	vars := map[string]string{"ip": "REMOTE_ADDR", "hostname": "REQUEST_HEADERS:Host", "url": "REQUEST_URI_RAW", "path": "REQUEST_FILENAME", "query": "QUERY_STRING", "method": "REQUEST_METHOD", "http_version": "REQUEST_PROTOCOL", "user_agent": "REQUEST_HEADERS:User-Agent", "referer": "REQUEST_HEADERS:Referer", "content_type": "REQUEST_HEADERS:Content-Type", "body": "REQUEST_BODY", "https": "HTTPS", "request_size": "FULL_REQUEST_LENGTH", "body_size": "REQUEST_BODY_LENGTH"}
	if v, ok := vars[c.Field]; ok {
		return v, nil
	}
	named := map[string]string{"header": "REQUEST_HEADERS:", "cookie": "REQUEST_COOKIES:", "argument": "ARGS:", "filename": "FILES:"}
	if v, ok := named[c.Field]; ok && wafHeaderKey.MatchString(c.Key) {
		return v + c.Key, nil
	}
	return "", fmt.Errorf("field %q is unavailable; named fields require a valid key", c.Field)
}

type wafTerm struct{ variable, operator string }

// Expand nested groups to a bounded disjunction of chains; NOT is expressed by not_* operators.
func wafConditionChains(c WAFCondition, depth int) ([][]wafTerm, error) {
	if depth > 5 {
		return nil, errors.New("rule groups may be at most five levels deep")
	}
	if c.Join == "not" {
		if len(c.Children) != 1 {
			return nil, errors.New("NOT groups need exactly one child")
		}
		negated, err := negateWAFCondition(c.Children[0], depth+1)
		if err != nil {
			return nil, err
		}
		return wafConditionChains(negated, depth+1)
	}
	if len(c.Children) > 0 {
		if len(c.Children) > 12 || (c.Join != "all" && c.Join != "any") {
			return nil, errors.New("choose all/any for groups of at most 12 conditions")
		}
		result := [][]wafTerm{{}}
		if c.Join == "any" {
			result = nil
		}
		for _, child := range c.Children {
			next, err := wafConditionChains(child, depth+1)
			if err != nil {
				return nil, err
			}
			if c.Join == "any" {
				result = append(result, next...)
			} else {
				var joined [][]wafTerm
				for _, a := range result {
					for _, b := range next {
						v := append(append([]wafTerm{}, a...), b...)
						if len(v) > 24 {
							return nil, errors.New("rule has too many combined conditions")
						}
						joined = append(joined, v)
					}
				}
				result = joined
			}
			if len(result) > 32 {
				return nil, errors.New("rule expansion exceeds 32 branches")
			}
		}
		return result, nil
	}
	v, err := wafVariable(c)
	if err != nil {
		return nil, err
	}
	if len(c.Value) > 1024 || strings.ContainsAny(c.Value, "\r\n\x00") {
		return nil, errors.New("rule values must be single-line and at most 1024 bytes")
	}
	op := c.Operator
	neg := ""
	if strings.HasPrefix(op, "not_") {
		neg = "!"
		op = strings.TrimPrefix(op, "not_")
	}
	value := c.Value
	switch op {
	case "equals":
		op = "@streq "
	case "contains":
		op = "@contains "
	case "starts_with":
		op = "@beginsWith "
	case "ends_with":
		op = "@endsWith "
	case "regex":
		if _, err = regexp.Compile(value); err != nil {
			return nil, errors.New("invalid regular expression")
		}
		op = "@rx "
	case "ip_in":
		if c.Field != "ip" {
			return nil, errors.New("IP membership applies only to client IP")
		}
		for _, ip := range strings.Split(value, ",") {
			ip = strings.TrimSpace(ip)
			if net.ParseIP(ip) == nil {
				if _, _, e := net.ParseCIDR(ip); e != nil {
					return nil, errors.New("invalid IP/CIDR list")
				}
			}
		}
		op = "@ipMatch "
	case "exists":
		v = "&" + v
		op = "@gt "
		value = "0"
	case "greater_than", "less_than":
		if _, e := strconv.ParseUint(value, 10, 64); e != nil {
			return nil, errors.New("numeric comparison requires a positive integer")
		}
		if op == "greater_than" {
			op = "@gt "
		} else {
			op = "@lt "
		}
	default:
		return nil, errors.New("unsupported operator")
	}
	return [][]wafTerm{{{v, neg + op + value}}}, nil
}
func validateWAFPolicy(p WAFPolicy) error {
	if p.Mode != "off" && p.Mode != "detect" && p.Mode != "block" {
		return errors.New("mode must be off, detect or block")
	}
	if len(p.Rules) > 100 || len(p.RateLimits) > 20 {
		return errors.New("maximum 100 custom rules and 20 rate limits per scope")
	}
	seen := map[string]bool{}
	for _, r := range p.Rules {
		if !wafIdentifier.MatchString(r.ID) || seen[r.ID] || len(strings.TrimSpace(r.Name)) == 0 || len(r.Name) > 100 || strings.ContainsAny(r.Name, "\r\n\x00") {
			return errors.New("rules need unique IDs and a name of at most 100 characters")
		}
		seen[r.ID] = true
		if r.Action != "block" && r.Action != "log" {
			return errors.New("supported actions are block and log")
		}
		if _, err := wafConditionChains(r.Match, 0); err != nil {
			return err
		}
	}
	for _, r := range p.RateLimits {
		if !wafIdentifier.MatchString(r.ID) || seen[r.ID] || r.Requests < 1 || r.Requests > 60000 || r.Burst < 0 || r.Burst > 10000 || len(r.Name) < 1 || len(r.Name) > 100 {
			return errors.New("invalid rate limit: use 1–60000 requests/minute and 0–10000 burst")
		}
		seen[r.ID] = true
		if !strings.HasPrefix(r.PathPrefix, "/") || len(r.PathPrefix) > 512 || strings.ContainsAny(r.PathPrefix, "\r\n\x00") {
			return errors.New("rate limit path must begin with / and contain at most 512 characters")
		}
	}
	return nil
}
func compileWAFCustom(p WAFPolicy, scope string, next *int) (string, error) {
	if err := validateWAFPolicy(p); err != nil {
		return "", err
	}
	if p.Mode == "off" {
		return "", nil
	}
	var out strings.Builder
	for _, r := range p.Rules {
		if !r.Enabled {
			continue
		}
		chains, err := wafConditionChains(r.Match, 0)
		if err != nil {
			return "", err
		}
		for _, chain := range chains {
			*next++
			action := "pass"
			if p.Mode == "block" && r.Action == "block" {
				action = "deny,status:403"
			}
			msg := scope + " rule " + r.ID + ": " + r.Name
			// Names are text, never action fragments. Strip action-list delimiters.
			msg = strings.Map(func(c rune) rune {
				if c == '\'' || c == '"' || c == '\\' || c < 32 {
					return '_'
				}
				return c
			}, msg)
			for i, t := range chain {
				actions := "t:none"
				if i == 0 {
					actions = fmt.Sprintf("id:%d,phase:2,%s,log,auditlog,t:none,msg:'%s',tag:'litecp-custom'", *next, action, msg)
				}
				if i < len(chain)-1 {
					actions += ",chain"
				}
				fmt.Fprintf(&out, "SecRule %s %s %s\n", t.variable, wafQuote(t.operator), wafQuote(actions))
			}
		}
	}
	return out.String(), nil
}
func effectiveWAFRules(p WAFPolicies, siteID string) string {
	local, ok := p.Websites[siteID]
	if !ok {
		local = defaultWAFPolicy()
	}
	mode := "DetectionOnly"
	if p.Server.Mode == "block" || local.Mode == "block" {
		mode = "On"
	}
	next := 7000000
	server, _ := compileWAFCustom(p.Server, "Server", &next)
	localRules, _ := compileWAFCustom(local, "Website", &next)
	managed := (p.Server.Mode != "off" && p.Server.Managed) || (local.Mode != "off" && local.Managed)
	out := "Include /etc/litecp/waf/modsecurity.conf\nSecRuleEngine " + mode + "\nSecAuditLog /var/log/nginx/litecp-waf-" + safeSiteID(siteID) + ".log\n" + server + localRules
	if managed {
		out += "Include /etc/litecp/waf/crs-setup.conf\nInclude /etc/litecp/waf/rules/*.conf\n"
	}
	return out
}
func wafRatePrefix(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return fmt.Sprintf("lcw_%x_", sum[:6])
}
func wafRateName(scope, id string) string {
	sum := sha256.Sum256([]byte(id))
	return wafRatePrefix(scope) + fmt.Sprintf("%x", sum[:6])
}

func wafRateDirectives(p WAFPolicies, siteID string) string {
	var out strings.Builder
	scopes := []struct {
		name string
		p    WAFPolicy
	}{{"server", p.Server}}
	if v, ok := p.Websites[siteID]; ok {
		scopes = append(scopes, struct {
			name string
			p    WAFPolicy
		}{safeSiteID(siteID), v})
	}
	for _, scope := range scopes {
		if scope.p.Mode == "off" {
			continue
		}
		for _, r := range scope.p.RateLimits {
			if !r.Enabled {
				continue
			}
			name := wafRateName(scope.name, r.ID)
			if r.Burst == 0 {
				fmt.Fprintf(&out, "    limit_req zone=%s;\n", name)
			} else {
				fmt.Fprintf(&out, "    limit_req zone=%s burst=%d nodelay;\n", name, r.Burst)
			}
		}
	}
	out.WriteString("    limit_req_status 429;\n")
	// Protection mode does not imply rate enforcement when that scope has no limiters.
	enforce := p.Server.Mode == "block" && hasWAFRates(p.Server)
	if local, ok := p.Websites[siteID]; ok && local.Mode == "block" && hasWAFRates(local) {
		enforce = true
	}
	if enforce {
		out.WriteString("    limit_req_dry_run off;\n")
	} else {
		out.WriteString("    limit_req_dry_run on;\n")
	}

	return out.String()
}
func compileWAFHTTP(p WAFPolicies) string {
	var out strings.Builder
	out.WriteString("# Generated by LiteCP. HTTP-context server WAF and rate zones.\n")
	scopes := []struct {
		name string
		p    WAFPolicy
	}{{"server", p.Server}}
	keys := []string{}
	for id := range p.Websites {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		scopes = append(scopes, struct {
			name string
			p    WAFPolicy
		}{safeSiteID(id), p.Websites[id]})
	}
	for _, scope := range scopes {
		if scope.p.Mode == "off" {
			continue
		}
		for _, r := range scope.p.RateLimits {
			if !r.Enabled {
				continue
			}
			name := wafRateName(scope.name, r.ID)
			fmt.Fprintf(&out, "map $uri $%s_key { default \"\"; %s $binary_remote_addr; }\nlimit_req_zone $%s_key zone=%s:1m rate=%dr/m;\n", name, wafQuote("~^"+regexp.QuoteMeta(r.PathPrefix)), name, name, r.Requests)
		}
	}
	if p.Server.Mode != "off" {
		// ModSecurity merges parent rules. Attach global rules to vhosts, not HTTP context.
		out.WriteString(wafRateDirectives(WAFPolicies{Server: p.Server}, "server"))
	}
	return out.String()
}

// Nginx include inheritance can be overridden by a vhost, so every managed site
// receives the mandatory server policy as well as its own policy.
func advancedWAFBlock(site Site) (string, bool) {
	if _, err := os.Stat(wafPolicyPath); err != nil {
		return "", false
	}
	p, err := readWAFPolicies(wafPolicyPath)
	if err != nil {
		return "", false
	}
	local, ok := p.Websites[site.ID]
	if !ok {
		local = defaultWAFPolicy()
	}
	enabled := p.Server.Mode != "off" || local.Mode != "off"
	block := "    # LITECP_WAF_BEGIN\n"
	if enabled {
		rulePath := "/etc/litecp/waf/site-" + safeSiteID(site.ID) + ".conf"
		if !ok {
			rulePath = wafGlobalPath
		}
		block += "    modsecurity on;\n    modsecurity_rules_file " + rulePath + ";\n" + wafRateDirectives(p, site.ID)
	} else {
		block += "    modsecurity off;\n"
	}
	return block + "    # LITECP_WAF_END", true
}
func (a *App) wafPolicies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := wafPolicyPath
	if a.system.demo {
		path = filepath.Join(filepath.Dir(a.store.path), "waf-policies.json")
	}
	p, err := readWAFPolicies(path)
	if err != nil {
		writeError(w, 500, "Cannot read WAF policies.")
		return
	}
	for _, site := range a.store.Sites() {
		if _, ok := p.Websites[site.ID]; !ok {
			v := defaultWAFPolicy()
			v.Mode = site.WAFMode
			if v.Mode == "" {
				v.Mode = "off"
			}
			v.Managed = true
			p.Websites[site.ID] = v
		}
	}
	if r.Method == http.MethodGet {
		running := a.system.demo
		if !running {
			output, runErr := runCommand(r.Context(), 3*time.Second, "systemctl", "is-active", "nginx")
			running = runErr == nil && strings.TrimSpace(output) == "active"
		}
		writeJSON(w, 200, map[string]any{"nginxRunning": running, "policies": p, "engine": a.system.WAFEngine(), "coverage": "Nginx HTTP/HTTPS listeners only. Direct Go listeners, SSH, mail and FTP are not inspected. Existing Nginx vhosts are configured on Apply; reapply after adding unmanaged vhosts. Verify endpoint coverage before production.", "rateLimitNote": "Per client IP and path prefix, requests/minute with burst. Shared Nginx zones; 429 until the bucket refills. Detect mode uses dry-run. Website Protect also enforces inherited server rate limits.", "unavailableFields": []string{"country", "asn", "client_certificate", "tls_version", "actual_mime"}})
		return
	}
	var input struct {
		Scope  string    `json:"scope"`
		Policy WAFPolicy `json:"policy"`
	}
	if err = decodeJSON(r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err = validateWAFPolicy(input.Policy); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if input.Scope != "server" && a.store.SiteByID(input.Scope) == nil {
		writeError(w, 404, "Website not found.")
		return
	}
	if input.Scope == "server" {
		p.Server = input.Policy
	} else {
		p.Websites[input.Scope] = input.Policy
	}
	if !a.system.demo && a.system.WAFEngine() == "not installed" {
		writeError(w, 409, "Install the WAF engine before applying policies.")
		return
	}
	// Nginx has one dry-run switch per vhost; reject mixed rate enforcement
	// rather than silently enforcing a Detect-only policy.
	for _, local := range p.Websites {
		if p.Server.Mode != "off" && local.Mode != "off" && p.Server.Mode != local.Mode && hasWAFRates(p.Server) && hasWAFRates(local) {
			writeError(w, 409, "Server and website rate limits on the same website must use the same mode. Nginx cannot independently dry-run one inherited limiter.")
			return
		}
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if a.system.demo {
		err = writeAtomic(path, data, 0600)
	} else {
		err = a.applyWAFPolicies(r.Context(), p, data)
	}
	if err != nil {
		writeError(w, 502, "WAF policy was not applied: "+err.Error())
		return
	}
	a.store.AddActivity("WAF policy updated", input.Scope+": "+input.Policy.Mode, "success")
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Policy validated and applied."})
}
func (a *App) applyWAFPolicies(ctx context.Context, p WAFPolicies, data []byte) error {
	updates := map[string][]byte{wafPolicyPath: data, wafGlobalPath: []byte(effectiveWAFRules(p, "server")), wafHTTPPath: []byte(compileWAFHTTP(p))}
	for _, site := range a.store.Sites() {
		updates["/etc/litecp/waf/site-"+safeSiteID(site.ID)+".conf"] = []byte(effectiveWAFRules(p, site.ID))
		config, _ := a.system.nginxPaths(site.Domain)
		original, err := os.ReadFile(config)
		if err != nil {
			return err
		}
		// Build directly from the proposed policy, without publishing it before validation.
		local := p.Websites[site.ID]
		block := "    # LITECP_WAF_BEGIN\n    modsecurity off;\n    # LITECP_WAF_END"
		if p.Server.Mode != "off" || local.Mode != "off" {
			block = "    # LITECP_WAF_BEGIN\n    modsecurity on;\n    modsecurity_rules_file /etc/litecp/waf/site-" + safeSiteID(site.ID) + ".conf;\n" + wafRateDirectives(p, site.ID) + "    # LITECP_WAF_END"
		}
		text := string(original)
		re := regexp.MustCompile(`(?s)    # LITECP_WAF_BEGIN.*?    # LITECP_WAF_END`)
		if re.MatchString(text) {
			text = re.ReplaceAllStringFunc(text, func(string) string { return block })
		} else {
			text = strings.ReplaceAll(text, "server {", "server {\n"+block)
		}
		updates[config] = []byte(text)
	}
	// Apply server protection to existing default, panel and webmail vhosts too.
	// Resolve symlinks inside /etc/nginx and never rewrite external linked files.
	for _, pattern := range []string{"/etc/nginx/conf.d/*.conf", "/etc/nginx/sites-enabled/*"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		for _, candidate := range paths {
			path, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(filepath.Clean(path), "/etc/nginx/") {
				return errors.New("Nginx include links outside /etc/nginx; server WAF needs manual review")
			}
			if _, exists := updates[path]; exists {
				continue
			}
			original, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(original)
			marker := regexp.MustCompile(`(?s)    # LITECP_SERVER_WAF_BEGIN.*?    # LITECP_SERVER_WAF_END`)
			without := marker.ReplaceAllString(text, "")
			if !regexp.MustCompile(`(?m)^\s*server\s*\{`).MatchString(without) {
				continue
			}
			if p.Server.Mode != "off" && strings.Contains(without, "modsecurity_rules_file") {
				return errors.New("Existing custom WAF configuration in " + path + " requires review before server policy can be added")
			}
			block := "    # LITECP_SERVER_WAF_BEGIN\n"
			if p.Server.Mode != "off" {
				block += "    modsecurity on;\n    modsecurity_rules_file " + wafGlobalPath + ";\n"
			}
			block += "    # LITECP_SERVER_WAF_END"
			if marker.MatchString(text) {
				text = marker.ReplaceAllStringFunc(text, func(string) string { return block })
			} else if p.Server.Mode != "off" {
				text = regexp.MustCompile(`(?m)^(\s*server\s*\{)`).ReplaceAllStringFunc(text, func(v string) string { return v + "\n" + block })
			} else {
				continue
			}
			updates[path] = []byte(text)
		}
	}
	type saved struct {
		path   string
		data   []byte
		mode   os.FileMode
		exists bool
	}
	old := []saved{}
	rollback := func(cause error) error {
		var failures []string
		for i := len(old) - 1; i >= 0; i-- {
			v := old[i]
			var err error
			if v.exists {
				err = writeAtomic(v.path, v.data, v.mode)
			} else {
				err = os.Remove(v.path)
				if os.IsNotExist(err) {
					err = nil
				}
			}
			if err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("%v; rollback: %s", cause, strings.Join(failures, "; "))
		}
		return cause
	}
	keys := []string{}
	for path := range updates {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		info, err := os.Lstat(path)
		mode := os.FileMode(0640)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return rollback(err)
		}
		var before []byte
		if exists {
			if !info.Mode().IsRegular() {
				return rollback(errors.New("WAF target is not a regular file"))
			}
			before, err = os.ReadFile(path)
			if err != nil {
				return rollback(err)
			}
			mode = info.Mode().Perm()
		}
		old = append(old, saved{path, before, mode, exists})
		if err = writeAtomic(path, updates[path], mode); err != nil {
			return rollback(err)
		}
	}
	if _, err := runCommand(ctx, 20*time.Second, "nginx", "-t"); err != nil {
		return rollback(err)
	}
	if _, err := runCommand(ctx, 20*time.Second, "systemctl", "reload", "nginx"); err != nil {
		cause := rollback(err)
		if _, testErr := runCommand(context.Background(), 20*time.Second, "nginx", "-t"); testErr != nil {
			return fmt.Errorf("%v; restored configuration validation failed: %v", cause, testErr)
		}
		if _, reloadErr := runCommand(context.Background(), 20*time.Second, "systemctl", "reload", "nginx"); reloadErr != nil {
			return fmt.Errorf("%v; restored configuration reload failed: %v", cause, reloadErr)
		}
		return cause
	}
	return nil
}

func (a *App) wafPolicyMethods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, 405, "Method not allowed.")
		return
	}
	a.wafPolicies(w, r)
}

func hasWAFRates(p WAFPolicy) bool {
	for _, r := range p.RateLimits {
		if r.Enabled {
			return true
		}
	}
	return false
}

func negateWAFCondition(c WAFCondition, depth int) (WAFCondition, error) {
	if depth > 5 {
		return c, errors.New("rule groups may be at most five levels deep")
	}
	if c.Join == "not" && len(c.Children) == 1 {
		return c.Children[0], nil
	}
	if len(c.Children) > 0 {
		if c.Join == "all" {
			c.Join = "any"
		} else if c.Join == "any" {
			c.Join = "all"
		} else {
			return c, errors.New("invalid condition group")
		}
		children := make([]WAFCondition, len(c.Children))
		for i, child := range c.Children {
			var err error
			children[i], err = negateWAFCondition(child, depth+1)
			if err != nil {
				return c, err
			}
		}
		c.Children = children
		return c, nil
	}
	if strings.HasPrefix(c.Operator, "not_") {
		c.Operator = strings.TrimPrefix(c.Operator, "not_")
	} else {
		c.Operator = "not_" + c.Operator
	}
	return c, nil
}
