package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebsiteDNSZoneRoundTrip(t *testing.T) {
	text := `$ORIGIN example.test.
$TTL 1h
@ IN SOA ns1.example.test. hostmaster.example.test. (
2026110301 3600 900 1209600 300 )
 IN NS ns1.example.test.
@ IN A 192.0.2.1
default._domainkey IN TXT "v=DKIM1; p=abc  def" "more"
www IN CNAME example.test.
`
	records, err := parseWebsiteDNS(text, "example.test")
	if err != nil {
		t.Fatal(err)
	}
	if records[3].Value != `"v=DKIM1; p=abc  def" "more"` {
		t.Fatalf("TXT changed: %q", records[3].Value)
	}
	zone := WebsiteDNSZone{Domain: "example.test", Records: records}
	if err = advanceDNSSerial(&zone, 2026110301); err != nil {
		t.Fatal(err)
	}
	if zone.Serial <= 2026110301 {
		t.Fatal("serial did not advance")
	}
	if _, err = parseWebsiteDNS(renderWebsiteDNS(zone), zone.Domain); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"$INCLUDE /etc/shadow\n", "$ORIGIN other.test.\n", "elsewhere.test. IN A 192.0.2.1\n"} {
		if _, err = parseWebsiteDNS(text+bad, "example.test"); err == nil {
			t.Fatal("unsafe zone accepted", bad)
		}
	}
	records[2].Value = "192.0.2.1\nother IN A 192.0.2.2"
	if validateWebsiteDNS("example.test", records) == nil {
		t.Fatal("record injection accepted")
	}
	config := dnsNamedConfig(map[string]string{"example.test": "/etc/litecp/dns/example.zone"})
	for _, boundary := range []string{"recursion no", "allow-transfer { none; }", "allow-update { none; }", "controls {}"} {
		if !strings.Contains(config, boundary) {
			t.Fatal("DNS boundary missing", boundary)
		}
	}
}

func TestWebsiteDNSPersistedAndCPanelImport(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	site := Site{ID: "dns_site_123", Domain: "example.test"}
	if err = store.AddSite(site); err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true, dataDir: dir}}
	root := t.TempDir()
	dns := filepath.Join(root, "cpanel_account", "dns")
	os.MkdirAll(dns, 0700)
	zone := defaultWebsiteDNS(site.Domain, "192.0.2.4")
	if err = os.WriteFile(filepath.Join(dns, site.Domain+".db"), []byte(renderWebsiteDNS(zone)), 0600); err != nil {
		t.Fatal(err)
	}
	job := LiveMigrationJob{Source: LiveSource{Platform: "cpanel", Host: "192.0.2.4"}}
	if err = app.importCPanelDNS(context.Background(), root, &job, site); err != nil {
		t.Fatal(err)
	}
	loaded, err := OpenStore(dir)
	if err != nil || loaded.SiteByID(site.ID).DNSZone == nil {
		t.Fatal("DNS not persisted", err)
	}
	saved := *store.SiteByID(site.ID)
	before := saved.DNSZone.Serial
	if err = app.importCPanelDNS(context.Background(), root, &job, saved); err != nil {
		t.Fatal(err)
	}
	if store.SiteByID(site.ID).DNSZone.Serial != before {
		t.Fatal("retry changed existing zone")
	}
	// Date-based source serials may be ahead of Unix time. Import must still
	// advance from that serial so resolvers do not keep the previous zone.
	imported := defaultWebsiteDNS(site.Domain, "192.0.2.4")
	imported.Records[0].Value = "ns1.example.test. hostmaster.example.test. 2099110301 3600 900 1209600 300"
	if err = app.saveWebsiteDNS(context.Background(), saved, imported); err != nil {
		t.Fatal(err)
	}
	if store.SiteByID(site.ID).DNSZone.Serial != 2099110302 {
		t.Fatal("source SOA serial moved backwards")
	}
	imported.Records[0].Value = "ns1.example.test. hostmaster.example.test. invalid 3600 900 1209600 300"
	if app.saveWebsiteDNS(context.Background(), *store.SiteByID(site.ID), imported) == nil {
		t.Fatal("invalid source SOA serial accepted")
	}
}

func TestWebsiteDNSRenameAndExport(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	zone := defaultWebsiteDNS("old.example.test", "192.0.2.1")
	zone.Records = append(zone.Records, WebsiteDNSRecord{"token", "TXT", 3600, `"keep old.example.test. unchanged"`})
	site := Site{ID: "dns-rename", Domain: zone.Domain, Root: "/var/www/litecp/old.example.test/public", DNSZone: &zone}
	if err = store.AddSite(site); err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true, dataDir: dir}}
	response := httptest.NewRecorder()
	app.changeSiteDomain(response, httptest.NewRequest(http.MethodPut, "/api/sites/domain", strings.NewReader(`{"id":"dns-rename","domain":"new.example.test"}`)))
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	saved := store.SiteByID(site.ID)
	if saved.DNSZone.Domain != "new.example.test" || !strings.Contains(saved.DNSZone.Records[0].Value, "ns1.new.example.test.") || saved.DNSZone.Serial <= zone.Serial {
		t.Fatal("renamed zone not updated")
	}
	if saved.DNSZone.Records[len(zone.Records)-1].Value != zone.Records[len(zone.Records)-1].Value {
		t.Fatal("TXT payload changed")
	}
	response = httptest.NewRecorder()
	app.websiteDNS(response, httptest.NewRequest(http.MethodGet, "/api/sites/dns?site=dns-rename&export=1", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "$ORIGIN new.example.test.") || !strings.Contains(response.Header().Get("Content-Disposition"), "new.example.test.zone") {
		t.Fatal("export failed", response.Body.String())
	}
	if _, err = parseWebsiteDNS(response.Body.String(), saved.Domain); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	app.websiteDNS(response, httptest.NewRequest(http.MethodPut, "/api/sites/dns?site=dns-rename", strings.NewReader(`{"records":[{"name":"@","type":"A","value":"127.0.0.1\n$INCLUDE /etc/shadow"}]}`)))
	if response.Code != 400 {
		t.Fatal("unsafe request accepted", response.Code)
	}
	original := renderWebsiteDNS(*store.SiteByID(site.ID).DNSZone)
	if _, err = app.prepareWebsiteDNSChange(context.Background(), *saved, nil); err != nil {
		t.Fatal(err)
	}
	if renderWebsiteDNS(*store.SiteByID(site.ID).DNSZone) != original {
		t.Fatal("unpublish preparation changed saved zone")
	}
}
