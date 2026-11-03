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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type WebsiteDNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	TTL   uint32 `json:"ttl"`
	Value string `json:"value"`
}
type WebsiteDNSZone struct {
	Domain    string             `json:"domain"`
	Records   []WebsiteDNSRecord `json:"records"`
	Serial    uint32             `json:"serial"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

var dnsRecordType = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}$`)
var dnsOwner = regexp.MustCompile(`^[A-Za-z0-9_.*-]+(?:\.[A-Za-z0-9_*-]+)*\.?$`)

// Split zone text without interpreting $INCLUDE, $GENERATE, escapes or code.
// Quoted TXT chunks, comments, inherited owners and multi-line SOA are supported.
func dnsZoneLines(text string) ([]string, error) {
	if len(text) > 2<<20 || strings.ContainsRune(text, 0) {
		return nil, errors.New("DNS zone exceeds 2 MiB or contains invalid text")
	}
	lines := []string{}
	var line strings.Builder
	quoted, escaped, comment := false, false, false
	depth := 0
	for _, c := range text {
		if c == '\r' {
			continue
		}
		if comment {
			if c != '\n' {
				continue
			}
			comment = false
		}
		if escaped {
			line.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			line.WriteRune(c)
			escaped = true
			continue
		}
		if c == '"' {
			quoted = !quoted
			line.WriteRune(c)
			continue
		}
		if !quoted {
			if c == ';' {
				comment = true
				continue
			}
			if c == '(' {
				depth++
				if depth > 1 {
					return nil, errors.New("nested DNS parentheses are not supported")
				}
				line.WriteByte(' ')
				continue
			}
			if c == ')' {
				depth--
				if depth < 0 {
					return nil, errors.New("unmatched DNS parentheses")
				}
				line.WriteByte(' ')
				continue
			}
			if c == '\n' {
				if depth > 0 {
					line.WriteByte(' ')
					continue
				}
				if strings.TrimSpace(line.String()) != "" {
					lines = append(lines, line.String())
				}
				line.Reset()
				continue
			}
		}
		if c == '\n' && quoted {
			return nil, errors.New("newline inside quoted DNS value")
		}
		if c < 32 && c != '\t' {
			return nil, errors.New("invalid DNS control character")
		}
		line.WriteRune(c)
	}
	if quoted || escaped || depth != 0 {
		return nil, errors.New("unterminated DNS value")
	}
	if strings.TrimSpace(line.String()) != "" {
		lines = append(lines, line.String())
	}
	return lines, nil
}
func parseWebsiteDNS(text, domain string) ([]WebsiteDNSRecord, error) {
	if validateMailDomain(domain) != nil {
		return nil, errors.New("invalid DNS domain")
	}
	lines, err := dnsZoneLines(text)
	if err != nil {
		return nil, err
	}
	records := []WebsiteDNSRecord{}
	owner := "@"
	ttl := uint32(3600)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if strings.HasPrefix(fields[0], "$") {
			switch strings.ToUpper(fields[0]) {
			case "$ORIGIN":
				if len(fields) != 2 || strings.TrimSuffix(strings.ToLower(fields[1]), ".") != domain {
					return nil, errors.New("DNS origin must match website domain")
				}
			case "$TTL":
				if len(fields) != 2 {
					return nil, errors.New("invalid default DNS TTL")
				}
				n, e := parseDNSTTL(fields[1])
				if e != nil {
					return nil, e
				}
				ttl = n
			default:
				return nil, errors.New("unsupported DNS directive; includes and generated records are not executed")
			}
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			owner = fields[0]
			fields = fields[1:]
		}
		recordTTL := ttl
		for len(fields) > 0 {
			if strings.EqualFold(fields[0], "IN") {
				fields = fields[1:]
				continue
			}
			if fields[0][0] >= '0' && fields[0][0] <= '9' {
				n, e := parseDNSTTL(fields[0])
				if e != nil {
					return nil, e
				}
				recordTTL = n
				fields = fields[1:]
				continue
			}
			break
		}
		if len(fields) < 2 {
			return nil, errors.New("DNS record needs a type and value")
		}
		// Keep quoted TXT whitespace intact. Fields is used only for the header.
		headerCount := len(strings.Fields(line)) - len(fields) + 1
		offset := 0
		for n := 0; n < headerCount; n++ {
			for offset < len(line) && (line[offset] == ' ' || line[offset] == '\t') {
				offset++
			}
			for offset < len(line) && line[offset] != ' ' && line[offset] != '\t' {
				offset++
			}
		}
		records = append(records, WebsiteDNSRecord{Name: owner, Type: strings.ToUpper(fields[0]), TTL: recordTTL, Value: strings.TrimSpace(line[offset:])})
		if len(records) > 10000 {
			return nil, errors.New("zone exceeds 10000 records")
		}
	}
	return records, validateWebsiteDNS(domain, records)
}
func parseDNSTTL(value string) (uint32, error) {
	if len(value) == 0 || len(value) > 32 {
		return 0, errors.New("invalid DNS TTL")
	}
	var total, current uint64
	digits := false
	for _, c := range strings.ToLower(value) {
		if c >= '0' && c <= '9' {
			current = current*10 + uint64(c-'0')
			digits = true
			if current > 2147483647 {
				return 0, errors.New("DNS TTL too large")
			}
			continue
		}
		mult := uint64(0)
		switch c {
		case 'w':
			mult = 604800
		case 'd':
			mult = 86400
		case 'h':
			mult = 3600
		case 'm':
			mult = 60
		case 's':
			mult = 1
		}
		if mult == 0 || !digits {
			return 0, errors.New("invalid DNS TTL")
		}
		total += current * mult
		current = 0
		digits = false
	}
	total += current
	if total > 2147483647 {
		return 0, errors.New("DNS TTL too large")
	}
	return uint32(total), nil
}
func validateWebsiteDNS(domain string, records []WebsiteDNSRecord) error {
	if len(records) == 0 || len(records) > 10000 {
		return errors.New("zone must have 1 to 10000 records")
	}
	soa, ns := 0, 0
	for _, r := range records {
		owner := strings.ToLower(r.Name)
		if owner != "@" && (!dnsOwner.MatchString(owner) || (strings.HasSuffix(owner, ".") && owner != domain+"." && !strings.HasSuffix(owner, "."+domain+"."))) {
			return fmt.Errorf("record owner %q is outside the website zone or invalid", r.Name)
		}
		if !dnsRecordType.MatchString(r.Type) || r.TTL > 2147483647 || strings.ContainsAny(r.Value, "\r\n\x00") || strings.TrimSpace(r.Value) == "" {
			return errors.New("invalid DNS record type, TTL or value")
		}
		// Do not allow a value to inject another statement, comment or directive.
		parsed, e := dnsZoneLines("x IN " + r.Type + " " + r.Value + "\n")
		if e != nil || len(parsed) != 1 || strings.TrimSpace(parsed[0]) != "x IN "+r.Type+" "+r.Value {
			return errors.New("invalid DNS record value")
		}
		switch r.Type {
		case "RRSIG", "DNSKEY", "NSEC", "NSEC3", "NSEC3PARAM":
			return errors.New("signed DNS zones require DNSSEC key migration and re-signing; export and review this zone before publishing")
		case "A":
			if ip := net.ParseIP(r.Value); ip == nil || ip.To4() == nil {
				return errors.New("A record requires an IPv4 address")
			}
		case "AAAA":
			if ip := net.ParseIP(r.Value); ip == nil || ip.To4() != nil {
				return errors.New("AAAA record requires an IPv6 address")
			}
		case "SOA":
			if owner != "@" && owner != domain+"." {
				return errors.New("SOA must be at the zone root")
			}
			soa++
			if len(strings.Fields(r.Value)) != 7 {
				return errors.New("SOA requires nameserver, contact and five timer values")
			}
		case "NS":
			if owner == "@" || owner == domain+"." {
				ns++
			}
		}
	}
	if soa != 1 || ns == 0 {
		return errors.New("zone requires exactly one SOA and at least one root NS record")
	}
	return nil
}
func defaultWebsiteDNS(domain, ip string) WebsiteDNSZone {
	serial := uint32(time.Now().Unix())
	records := []WebsiteDNSRecord{{"@", "SOA", 3600, fmt.Sprintf("ns1.%s. hostmaster.%s. %d 3600 900 1209600 300", domain, domain, serial)}, {"@", "NS", 3600, "ns1." + domain + "."}}
	if net.ParseIP(ip) != nil {
		kind := "A"
		if net.ParseIP(ip).To4() == nil {
			kind = "AAAA"
		}
		records = append(records, WebsiteDNSRecord{"@", kind, 3600, ip}, WebsiteDNSRecord{"ns1", kind, 3600, ip}, WebsiteDNSRecord{"www", "CNAME", 3600, domain + "."})
	}
	return WebsiteDNSZone{Domain: domain, Records: records, Serial: serial, UpdatedAt: time.Now().UTC()}
}
func renderWebsiteDNS(zone WebsiteDNSZone) string {
	var out strings.Builder
	fmt.Fprintf(&out, "$ORIGIN %s.\n$TTL 3600\n", zone.Domain)
	for _, r := range zone.Records {
		fmt.Fprintf(&out, "%s %d IN %s %s\n", r.Name, r.TTL, r.Type, r.Value)
	}
	return out.String()
}
func advanceDNSSerial(zone *WebsiteDNSZone, previous uint32) error {
	next := uint32(time.Now().Unix())
	if previous != 0 {
		next = previous + 1
	}
	if next == 0 {
		return errors.New("DNS serial exhausted")
	}
	zone.Serial = next
	zone.UpdatedAt = time.Now().UTC()
	for i := range zone.Records {
		if zone.Records[i].Type == "SOA" {
			parts := strings.Fields(zone.Records[i].Value)
			if len(parts) != 7 {
				return errors.New("invalid SOA")
			}
			parts[2] = strconv.FormatUint(uint64(next), 10)
			zone.Records[i].Value = strings.Join(parts, " ")
		}
	}
	return nil
}
func (s *System) dnsRoot() string {
	if s.demo {
		return filepath.Join(s.dataDir, "dns")
	}
	return "/etc/litecp/dns"
}
func (s *System) dnsAvailable() bool {
	if s.demo {
		return true
	}
	_, err := exec.LookPath("named-checkzone")
	_, e := os.Stat(filepath.Join(s.dnsRoot(), "enabled"))
	return err == nil && e == nil
}
func dnsSiteFile(site Site) string {
	sum := sha256.Sum256([]byte(site.ID))
	return fmt.Sprintf("%x.zone", sum[:16])
}
func dnsNamedConfig(zones map[string]string) string {
	var out strings.Builder
	out.WriteString("options { directory \"/etc/litecp/dns\"; recursion no; allow-recursion { none; }; allow-query-cache { none; }; allow-query { any; }; allow-transfer { none; }; dnssec-validation no; include \"/etc/litecp/dns/listen.conf\"; managed-keys-directory \"/run/litecp-dns\"; pid-file \"/run/litecp-dns/named.pid\"; session-keyfile \"/run/litecp-dns/session.key\"; };\ncontrols {};\n")
	// Deterministic configuration makes review and rollback straightforward.
	for _, domain := range sortedDNSDomains(zones) {
		fmt.Fprintf(&out, "zone %q { type master; file %q; allow-update { none; }; };\n", domain, zones[domain])
	}
	return out.String()
}
func sortedDNSDomains(zones map[string]string) []string {
	keys := make([]string, 0, len(zones))
	for k := range zones {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Validate the complete candidate configuration before replacing the live one.
// Immutable revisions are retained for recovery; unrelated named configuration is untouched.
func (a *App) saveWebsiteDNS(ctx context.Context, site Site, zone WebsiteDNSZone) error {
	if err := validateWebsiteDNS(site.Domain, zone.Records); err != nil {
		return err
	}
	zone.Domain = site.Domain
	previous := uint32(0)
	if site.DNSZone != nil {
		previous = site.DNSZone.Serial
	}
	// Never move an imported SOA serial backwards relative to its source zone.
	for _, record := range zone.Records {
		if record.Type == "SOA" {
			parts := strings.Fields(record.Value)
			serial, err := strconv.ParseUint(parts[2], 10, 32)
			if err != nil {
				return errors.New("SOA serial must be a 32-bit unsigned integer")
			}
			if uint32(serial) > previous {
				previous = uint32(serial)
			}
		}
	}
	if err := advanceDNSSerial(&zone, previous); err != nil {
		return err
	}
	sites := a.store.Sites()
	found := false
	for i := range sites {
		if sites[i].ID == site.ID {
			sites[i].DNSZone = &zone
			found = true
		}
	}
	if !found {
		return errors.New("website no longer exists")
	}
	rollback, err := a.publishWebsiteDNS(ctx, sites)
	if err != nil {
		return err
	}
	if err = a.store.UpdateSite(site.ID, func(s *Site) { s.DNSZone = &zone }); err != nil {
		return rollback(err)
	}
	return nil
}

// Publishing returns a rollback action so website lifecycle operations can keep
// authoritative routing and persisted website state in the same transaction.
func (a *App) publishWebsiteDNS(ctx context.Context, sites []Site) (func(error) error, error) {
	noop := func(err error) error { return err }
	if a.system.demo || !a.system.dnsAvailable() {
		return noop, nil
	}
	root := a.system.dnsRoot()
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	revision, err := os.MkdirTemp(root, "revision-")
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(revision, 0755); err != nil {
		return nil, err
	}
	zones := map[string]string{}
	for _, item := range sites {
		if item.DNSZone == nil {
			continue
		}
		if item.DNSZone.Domain != item.Domain {
			return nil, errors.New("saved DNS zone belongs to a previous domain; review it before publishing")
		}
		filename := filepath.Join(revision, dnsSiteFile(item))
		if err = writeAtomic(filename, []byte(renderWebsiteDNS(*item.DNSZone)), 0644); err != nil {
			return nil, err
		}
		if !a.system.demo {
			if output, e := runCommand(ctx, 20*time.Second, "named-checkzone", item.Domain, filename); e != nil {
				return nil, fmt.Errorf("DNS zone validation failed: %s", strings.TrimSpace(output))
			}
		}
		zones[item.Domain] = filename
	}
	config := []byte(dnsNamedConfig(zones))
	candidate := filepath.Join(revision, "named.conf")
	if err = writeAtomic(candidate, config, 0644); err != nil {
		return nil, err
	}
	if !a.system.demo {
		if output, e := runCommand(ctx, 20*time.Second, "named-checkconf", candidate); e != nil {
			return nil, fmt.Errorf("DNS configuration invalid: %s", strings.TrimSpace(output))
		}
		if _, e := exec.LookPath("restorecon"); e == nil {
			if _, e = runCommand(ctx, 20*time.Second, "restorecon", "-R", root); e != nil {
				return nil, errors.New("cannot apply DNS SELinux labels; run the DNS installer")
			}
		}
	}
	live := filepath.Join(root, "named.conf")
	old, readErr := os.ReadFile(live)
	if readErr != nil {
		return nil, errors.New("DNS initial configuration is missing; run scripts/install-dns.sh")
	}
	if err = writeAtomic(live, config, 0644); err != nil {
		return nil, err
	}
	rollback := func(cause error) error {
		if e := writeAtomic(live, old, 0644); e != nil {
			return fmt.Errorf("%v; DNS rollback failed: %w", cause, e)
		}
		if !a.system.demo {
			if _, e := runCommand(context.Background(), 20*time.Second, "systemctl", "reload", "litecp-dns"); e != nil {
				return fmt.Errorf("%v; DNS rollback reload failed: %w", cause, e)
			}
		}
		return cause
	}
	if !a.system.demo {
		if _, err = runCommand(ctx, 30*time.Second, "systemctl", "reload", "litecp-dns"); err != nil {
			return nil, rollback(errors.New("DNS reload failed; previous configuration restored"))
		}
	}
	return rollback, nil
}
func (a *App) websiteDNS(w http.ResponseWriter, r *http.Request) {
	site := a.store.SiteByID(r.URL.Query().Get("site"))
	if site == nil {
		writeError(w, 404, "Website not found.")
		return
	}
	zone := defaultWebsiteDNS(site.Domain, primaryIP())
	if site.DNSZone != nil {
		zone = *site.DNSZone
	}
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("export") == "1" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", site.Domain+".zone"))
			_, _ = w.Write([]byte(renderWebsiteDNS(zone)))
			return
		}
		active := false
		if !a.system.demo && a.system.dnsAvailable() {
			out, err := runCommand(r.Context(), 5*time.Second, "systemctl", "is-active", "litecp-dns")
			active = err == nil && strings.TrimSpace(out) == "active"
		}
		writeJSON(w, 200, map[string]any{"zone": zone, "installed": a.system.dnsAvailable(), "active": active, "saved": site.DNSZone != nil, "serverIP": primaryIP()})
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, 405, "Use GET or PUT.")
		return
	}
	var request struct {
		Records  []WebsiteDNSRecord `json:"records"`
		ZoneText string             `json:"zoneText"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3<<20)).Decode(&request); err != nil {
		writeError(w, 400, "Invalid DNS request.")
		return
	}
	if request.ZoneText != "" {
		records, err := parseWebsiteDNS(request.ZoneText, site.Domain)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		request.Records = records
	}
	if err := validateWebsiteDNS(site.Domain, request.Records); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// withAuth serializes mutations; do not acquire its mutex twice.
	site = a.store.SiteByID(site.ID)
	if site == nil {
		writeError(w, 404, "Website not found.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := a.saveWebsiteDNS(ctx, *site, WebsiteDNSZone{Records: request.Records}); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	a.store.AddActivity("DNS updated", site.Domain, "info")
	writeJSON(w, 200, map[string]any{"ok": true, "published": a.system.dnsAvailable()})
}

func (a *App) importCPanelDNS(ctx context.Context, root string, job *LiveMigrationJob, site Site) error {
	if site.DNSZone != nil {
		job.Warnings = append(job.Warnings, "Existing website DNS records were preserved; review them before cutover.")
		return nil
	}
	base := filepath.Join(root, "cpanel_account")
	entries, err := os.ReadDir(filepath.Join(base, "dns"))
	if os.IsNotExist(err) {
		job.Warnings = append(job.Warnings, "This capture has no DNS zone export. Recapture the account to import source DNS; public DNS has not been changed.")
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("invalid DNS export directory")
		}
		name := strings.TrimSuffix(entry.Name(), ".db")
		if name != site.Domain {
			continue
		}
		data, e := readCPanelMetadata(base, "dns/"+entry.Name(), false)
		if e != nil {
			return e
		}
		records, e := parseWebsiteDNS(string(data), site.Domain)
		if e != nil {
			return fmt.Errorf("source DNS needs review: %w", e)
		}
		// Preserve all records, including mail authentication. Repoint only addresses
		// equal to the source server; unrelated external services remain unchanged.
		targetIP := net.ParseIP(primaryIP())
		sourceIP := net.ParseIP(job.Source.Host)
		if targetIP != nil && sourceIP != nil {
			for i := range records {
				if ip := net.ParseIP(records[i].Value); ip != nil && ip.Equal(sourceIP) {
					records[i].Value = targetIP.String()
					records[i].Type = "A"
					if targetIP.To4() == nil {
						records[i].Type = "AAAA"
					}
				}
			}
		}
		if e = a.saveWebsiteDNS(ctx, site, WebsiteDNSZone{Records: records}); e != nil {
			return e
		}
		if !a.system.dnsAvailable() {
			job.Warnings = append(job.Warnings, "DNS records imported as a draft. Install the LiteCP DNS service before using it as an authoritative nameserver.")
		}
		job.Warnings = append(job.Warnings, "Source DNS records imported. Review NS/glue and registrar delegation before DNS cutover.")
		return nil
	}
	job.Warnings = append(job.Warnings, "No zone matching the migrated domain was present in the source DNS export.")
	return nil
}

// Rewrite only DNS owner/target names in this zone. TXT payloads and external
// names remain intact and can be reviewed before changing registrar delegation.
func renameWebsiteDNS(zone *WebsiteDNSZone, oldDomain, newDomain string) *WebsiteDNSZone {
	if zone == nil {
		return nil
	}
	next := *zone
	next.Domain = newDomain
	next.Records = append([]WebsiteDNSRecord{}, zone.Records...)
	rewrite := func(value string) string {
		lower := strings.ToLower(value)
		if lower == oldDomain+"." {
			return newDomain + "."
		}
		if strings.HasSuffix(lower, "."+oldDomain+".") {
			return value[:len(value)-len(oldDomain)-1] + newDomain + "."
		}
		return value
	}
	for i := range next.Records {
		r := &next.Records[i]
		r.Name = rewrite(r.Name)
		fields := strings.Fields(r.Value)
		indexes := []int{}
		switch r.Type {
		case "NS", "CNAME", "DNAME", "PTR":
			indexes = []int{0}
		case "MX":
			indexes = []int{1}
		case "SRV":
			indexes = []int{3}
		case "SOA":
			indexes = []int{0, 1}
		}
		for _, j := range indexes {
			if j < len(fields) {
				fields[j] = rewrite(fields[j])
			}
		}
		if len(indexes) > 0 {
			r.Value = strings.Join(fields, " ")
		}
	}
	return &next
}
func (a *App) prepareWebsiteDNSChange(ctx context.Context, site Site, replacement *Site) (func(error) error, error) {
	noop := func(err error) error { return err }
	if site.DNSZone == nil {
		return noop, nil
	}
	sites := []Site{}
	for _, item := range a.store.Sites() {
		if item.ID != site.ID {
			sites = append(sites, item)
		} else if replacement != nil {
			sites = append(sites, *replacement)
		}
	}
	return a.publishWebsiteDNS(ctx, sites)
}
