package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWAFWebsiteSwitchPreservesRulesAndMode(t *testing.T) {
	local := defaultWAFPolicy()
	local.Mode = "block"
	local.Managed = true
	local.Rules = []WAFCustomRule{wafTestRule("block")}
	local.RateLimits = []WAFRateLimit{{ID: "login", Enabled: true, PathPrefix: "/login", Requests: 20}}
	p := WAFPolicies{Server: local, Websites: map[string]WAFPolicy{"one": local, "two": local, "other": local}}
	setWAFWebsiteEnabled(&p, []string{"one", "two"}, false)
	if !wafWebsiteDisabled(p, "one") || wafWebsiteDisabled(p, "other") {
		t.Fatal("switch changed the wrong websites")
	}
	if p.Websites["one"].Mode != "block" || len(p.Websites["one"].Rules) != 1 || len(p.Websites["one"].RateLimits) != 1 {
		t.Fatal("switch destroyed configured settings")
	}
	if effectiveWAFRules(p, "one") != "SecRuleEngine Off\n" || !strings.Contains(wafRateDirectives(p, "one"), "dry_run on") {
		t.Fatal("disabled website still enforces inherited WAF settings")
	}
	setWAFWebsiteEnabled(&p, []string{"one"}, true)
	if wafWebsiteDisabled(p, "one") || p.Websites["one"].Mode != "block" || !strings.Contains(effectiveWAFRules(p, "one"), "deny,status:403") {
		t.Fatal("enable did not restore the configured policy")
	}
}

func TestWAFWebsiteSwitchLegacyAndPersistence(t *testing.T) {
	p := WAFPolicies{Server: defaultWAFPolicy(), Websites: map[string]WAFPolicy{"one": defaultWAFPolicy()}}
	if wafWebsiteDisabled(p, "one") {
		t.Fatal("legacy policy unexpectedly disabled")
	}
	setWAFWebsiteEnabled(&p, []string{"one"}, true)
	if p.Websites["one"].Mode != "detect" || !p.Websites["one"].Managed {
		t.Fatal("first enable must use managed detection")
	}
	setWAFWebsiteEnabled(&p, []string{"one"}, false)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var restored WAFPolicies
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !wafWebsiteDisabled(restored, "one") {
		t.Fatal("disabled switch was lost after reload")
	}
}

func wafTestRule(action string) WAFCustomRule {
	return WAFCustomRule{ID: "test", Name: "Admin path", Enabled: true, Action: action, Match: WAFCondition{Field: "path", Operator: "starts_with", Value: "/admin"}}
}
func TestWAFPolicyConditionExpansion(t *testing.T) {
	c := WAFCondition{Join: "all", Children: []WAFCondition{{Field: "method", Operator: "equals", Value: "POST"}, {Join: "any", Children: []WAFCondition{{Field: "path", Operator: "starts_with", Value: "/login"}, {Field: "path", Operator: "equals", Value: "/admin"}}}}}
	chains, err := wafConditionChains(c, 0)
	if err != nil || len(chains) != 2 || len(chains[0]) != 2 {
		t.Fatalf("unexpected expansion: %#v %v", chains, err)
	}
}
func TestWAFPolicyRejectsUnsafeAndUnavailableFields(t *testing.T) {
	tests := []WAFCondition{{Field: "header", Key: "Host\nSecRuleEngine Off", Operator: "equals", Value: "x"}, {Field: "path", Operator: "equals", Value: "/\nSecRuleEngine Off"}, {Field: "ip", Operator: "ip_in", Value: "not-an-ip"}, {Field: "country", Operator: "equals", Value: "IN"}, {Field: "path", Operator: "regex", Value: "["}, {Field: "body_size", Operator: "greater_than", Value: "bad"}, {Join: "all"}}
	for _, c := range tests {
		if _, err := wafConditionChains(c, 0); err == nil {
			t.Fatalf("accepted unsafe condition: %#v", c)
		}
	}
}
func TestWAFPolicyBoundsExpansion(t *testing.T) {
	any := WAFCondition{Join: "any"}
	for i := 0; i < 6; i++ {
		any.Children = append(any.Children, WAFCondition{Field: "path", Operator: "equals", Value: "/"})
	}
	if _, err := wafConditionChains(WAFCondition{Join: "all", Children: []WAFCondition{any, any}}, 0); err == nil {
		t.Fatal("accepted unbounded chain expansion")
	}
}
func TestWAFPolicyDetectNeverCompilesCustomDeny(t *testing.T) {
	p := defaultWAFPolicy()
	p.Mode = "detect"
	p.Rules = []WAFCustomRule{wafTestRule("block")}
	next := 7000000
	rules, err := compileWAFCustom(p, "Server", &next)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rules, "deny") || !strings.Contains(rules, "pass,log,auditlog") {
		t.Fatal(rules)
	}
	p.Mode = "block"
	rules, err = compileWAFCustom(p, "Server", &next)
	if err != nil || !strings.Contains(rules, "deny,status:403") {
		t.Fatalf("%s %v", rules, err)
	}
}
func TestWAFPolicyServerRulesAlwaysPrecedeWebsiteRules(t *testing.T) {
	server := defaultWAFPolicy()
	server.Mode = "block"
	server.Rules = []WAFCustomRule{wafTestRule("block")}
	local := defaultWAFPolicy()
	local.Mode = "detect"
	local.Rules = []WAFCustomRule{wafTestRule("block")}
	p := WAFPolicies{Server: server, Websites: map[string]WAFPolicy{"site": local}}
	rules := effectiveWAFRules(p, "site")
	if strings.Index(rules, "Server rule") >= strings.Index(rules, "Website rule") {
		t.Fatal(rules)
	}
	if strings.Count(rules, "deny,status:403") != 1 {
		t.Fatal("website detect policy became blocking: " + rules)
	}
}
func TestWAFPolicyRateZonesAndModes(t *testing.T) {
	server := defaultWAFPolicy()
	server.Mode = "block" // custom WAF protection without server rate limits
	local := defaultWAFPolicy()
	local.Mode = "detect"
	local.RateLimits = []WAFRateLimit{{ID: "login", Name: "Login", Enabled: true, PathPrefix: "/login", Requests: 30, Burst: 0}}
	p := WAFPolicies{Server: server, Websites: map[string]WAFPolicy{"site": local}}
	if err := validateWAFPolicy(local); err != nil {
		t.Fatal(err)
	}
	http := compileWAFHTTP(p)
	if !strings.Contains(http, "rate=30r/m") || strings.Contains(http, "modsecurity_rules_file") {
		t.Fatal("invalid HTTP policy: " + http)
	}
	rules := wafRateDirectives(p, "site")
	if !strings.Contains(rules, "limit_req_dry_run on;") || strings.Contains(rules, "burst=0") {
		t.Fatal(rules)
	}
	local.Mode = "block"
	p.Websites["site"] = local
	if !strings.Contains(wafRateDirectives(p, "site"), "dry_run off") {
		t.Fatal("protect limiter not enforced")
	}
	if wafRateName("site", "a-b") == wafRateName("site", "a_b") {
		t.Fatal("zone name collision")
	}
}
func TestWAFPolicyNameCannotInjectActions(t *testing.T) {
	p := defaultWAFPolicy()
	p.Mode = "block"
	r := wafTestRule("log")
	r.Name = "test',deny,status:200,msg:'oops"
	p.Rules = []WAFCustomRule{r}
	next := 7000000
	out, err := compileWAFCustom(p, "Server", &next)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "test',deny") {
		t.Fatal("unescaped action delimiter: " + out)
	}
}
func TestWAFPolicyRateValidation(t *testing.T) {
	p := defaultWAFPolicy()
	p.RateLimits = []WAFRateLimit{{ID: "a", Name: "Test", Enabled: true, PathPrefix: "/", Requests: 0}}
	if validateWAFPolicy(p) == nil {
		t.Fatal("accepted zero request limit")
	}
	p.RateLimits[0].Requests = 10
	p.RateLimits[0].PathPrefix = "/\n}"
	if validateWAFPolicy(p) == nil {
		t.Fatal("accepted injected path")
	}
}

func TestWAFPolicyNotGroupUsesDeMorgan(t *testing.T) {
	c := WAFCondition{Join: "not", Children: []WAFCondition{{Join: "any", Children: []WAFCondition{{Field: "method", Operator: "equals", Value: "GET"}, {Field: "method", Operator: "equals", Value: "POST"}}}}}
	chains, err := wafConditionChains(c, 0)
	if c.Children[0].Children[0].Operator != "equals" {
		t.Fatal("compiler mutated the original policy")
	}
	if err != nil || len(chains) != 1 || len(chains[0]) != 2 {
		t.Fatalf("%#v %v", chains, err)
	}
	for _, term := range chains[0] {
		if !strings.HasPrefix(term.operator, "!") {
			t.Fatal(term)
		}
	}
}
