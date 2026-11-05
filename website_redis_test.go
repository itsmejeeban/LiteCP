package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedisPrivateCacheConfiguration(t *testing.T) {
	base := nativeResourcePlan()
	p := redisPlan{Site: base.Site, Settings: WebsiteRedis{Enabled: true, MemoryMiB: 64}, Action: "save"}
	if err := validateRedisPlan(p); err != nil {
		t.Fatal(err)
	}
	config := redisConfig(p)
	for _, value := range []string{"port 0\n", "unixsocket " + redisSocket(p.Site.ID), "unixsocketperm 600", "maxmemory 64mb", "maxmemory-policy allkeys-lru", "save \"\"", "appendonly no"} {
		if !strings.Contains(config, value) {
			t.Fatalf("missing %s", value)
		}
	}
	unit := redisService(p, "/usr/bin/redis-server")
	for _, value := range []string{"DynamicUser=yes", "User=", "MemoryMax=128M", "MemorySwapMax=0", "NoNewPrivileges=yes", "RestrictAddressFamilies=AF_UNIX", "RuntimeDirectoryMode=0700", "Restart=no"} {
		if !strings.Contains(unit, value) {
			t.Fatalf("missing %s", value)
		}
	}
	if strings.Contains(unit, "Slice=") {
		t.Fatal("disabled website unexpectedly inherits resource caps")
	}
	p.Site.ResourceLimits = &base.Policy
	if !strings.Contains(redisService(p, "/usr/bin/redis-server"), "Slice="+resourceSliceName(p.Site.ID)) {
		t.Fatal("Redis escaped site slice")
	}
}
func TestRedisRejectsUnsafeAndUnsupportedPlans(t *testing.T) {
	p := redisPlan{Site: nativeResourcePlan().Site, Settings: WebsiteRedis{Enabled: true, MemoryMiB: 64}, Action: "save"}
	tests := []redisPlan{}
	v := p
	v.Site.ID = "../../root"
	tests = append(tests, v)
	v = p
	v.Site.RuntimeUID = 0
	tests = append(tests, v)
	v = p
	v.Settings.MemoryMiB = 0
	tests = append(tests, v)
	v = p
	v.Action = "flushall"
	tests = append(tests, v)
	v = p
	v.Site.PHPVersion = "7.3"
	tests = append(tests, v)
	v = p
	v.Site.Suspended = true
	tests = append(tests, v)
	for _, v := range tests {
		if validateRedisPlan(v) == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
	p.Site.Suspended = true
	p.Action = "status"
	if err := validateRedisPlan(p); err != nil {
		t.Fatal("status unavailable during suspension", err)
	}
	p.Action = "save"
	p.Settings.Enabled = false
	if err := validateRedisPlan(p); err != nil {
		t.Fatal("cannot disable during suspension", err)
	}
}
func TestRedisSettingsPersistWithWebsite(t *testing.T) {
	site := nativeResourcePlan().Site
	site.Redis = &WebsiteRedis{Enabled: true, MemoryMiB: 32}
	b, e := json.Marshal(site)
	if e != nil {
		t.Fatal(e)
	}
	var restored Site
	if json.Unmarshal(b, &restored) != nil || restored.Redis == nil || *restored.Redis != *site.Redis {
		t.Fatal("site serialization lost Redis settings")
	}
}

func TestRedisPHPAllowlistIncludesOnlyOwnSocketDirectory(t *testing.T) {
	s := &System{os: OSInfo{ID: "almalinux"}}
	a := nativeResourcePlan().Site
	b := a
	b.ID = "other-audit"
	cfg := s.phpPoolConfig(a)
	if !strings.Contains(cfg, filepath.Dir(redisSocket(a.ID))) || strings.Contains(cfg, filepath.Dir(redisSocket(b.ID))) {
		t.Fatal("PHP Redis socket boundary missing or too broad")
	}
	if strings.Contains(cfg, "open_basedir] = /run:") {
		t.Fatal("entire runtime tree exposed")
	}
}

func TestRedisCacheCannotChangeAdministrativeConfiguration(t *testing.T) {
	config := redisConfig(redisPlan{Site: nativeResourcePlan().Site, Settings: WebsiteRedis{MemoryMiB: 32}})
	for _, denied := range []string{"-config", "-acl", "-module", "-replicaof", "-bgrewriteaof"} {
		if !strings.Contains(config, denied) {
			t.Fatalf("missing command restriction %s", denied)
		}
	}
}
