package main

// Website traffic is origin-request analytics, not browser instrumentation.
// Checkpoints and counters are committed in one snapshot so a restart cannot
// advance an offset without also saving the requests counted at that offset.
import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const trafficMaxState = 16 << 20
const trafficMaxLine = 32 << 10
const trafficReadBudget = 256 << 10
const trafficDimensions = 80

// Nginx maps strip query strings before writing the dedicated analytics log.
// Operational access/error logs keep their existing format and configuration.
const trafficNginxFormat = `# Managed by LiteCP website traffic analytics (http context).
map $request_uri $litecp_traffic_path {
    default "/";
    "~^(?<litecp_traffic_clean_path>[^?]*)" $litecp_traffic_clean_path;
}
map $http_referer $litecp_traffic_referrer {
    default "";
    "~*^https?://(?<litecp_traffic_ref_host>[^/:?#]+)" $litecp_traffic_ref_host;
}
log_format litecp_traffic escape=json '{"id":"$request_id","time":"$time_iso8601","path":"$litecp_traffic_path","method":"$request_method","status":"$status","bytes":"$body_bytes_sent","duration":"$request_time","referrer":"$litecp_traffic_referrer","agent":"$http_user_agent","address":"$remote_addr","contentType":"$sent_http_content_type","protocol":"$server_protocol","requestBytes":"$request_length","cache":"$upstream_cache_status"}';
`

func trafficLogDirective(site Site) string {
	if home := siteHome(site); home != "" {
		return fmt.Sprintf("    access_log %s litecp_traffic buffer=32k flush=1s;\n", filepath.ToSlash(filepath.Join(home, "logs", "traffic-analytics.log")))
	}
	return ""
}

// A separate reconciler can upgrade existing HTTP and HTTPS server blocks
// without regenerating certificate directives or changing custom routing.
func injectTrafficLogging(content string, site Site) string {
	directive := strings.TrimSpace(trafficLogDirective(site))
	if directive == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		// Replace only our own directives. Existing operational logs are retained.
		if strings.Contains(line, "traffic-analytics.log") && strings.Contains(line, "litecp_traffic") {
			continue
		}
		if strings.TrimSpace(line) == "access_log off;" {
			// An explicit analytics destination overrides inherited operational
			// logs. Keeping "off" here would suppress analytics as well.
			out = append(out, "        # LiteCP analytics: operational access logging remains disabled", "        "+directive)
			continue
		}
		out = append(out, line)
		if strings.TrimSpace(line) == "# LiteCP analytics: operational access logging remains disabled" {
			out = append(out, "        "+directive)
		}
		if strings.TrimSpace(line) == "server {" {
			out = append(out, "    "+directive)
		}
		// Existing static-asset locations suppress their inherited logs. Add an
		// explicit analytics log there without re-enabling the operational log.
	}
	return strings.Join(out, "\n")
}

func (s *System) reconcileTrafficLogging(ctx context.Context, sites []Site) error {
	if s.demo {
		return nil
	}
	formatPath := "/etc/nginx/conf.d/00-litecp-traffic.conf"
	type change struct {
		path    string
		old     []byte
		existed bool
	}
	var changes []change
	write := func(path string, next []byte) error {
		old, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if string(old) == string(next) {
			return nil
		}
		if path == formatPath && err == nil && !strings.HasPrefix(string(old), "# Managed by LiteCP website traffic analytics") && !strings.HasPrefix(string(old), "# LiteCP traffic logging setup did not complete.") {
			return errors.New("analytics format path contains an unmanaged configuration; existing file retained")
		}
		changes = append(changes, change{path, old, err == nil})
		return writeAtomic(path, next, 0644)
	}
	recoverChanges := func(cause error) error {
		var recovery []string
		for i := len(changes) - 1; i >= 0; i-- {
			c := changes[i]
			// Keep newly created files as empty, harmless configuration rather
			// than deleting anything during automatic recovery.
			old := c.old
			if !c.existed {
				old = []byte("# LiteCP traffic logging setup did not complete.\n")
			}
			if err := writeAtomic(c.path, old, 0644); err != nil {
				recovery = append(recovery, err.Error())
			}
		}
		if len(recovery) > 0 {
			return fmt.Errorf("%v; configuration recovery: %s", cause, strings.Join(recovery, "; "))
		}
		return cause
	}
	if err := write(formatPath, []byte(trafficNginxFormat)); err != nil {
		return recoverChanges(err)
	}
	for _, site := range sites {
		if siteHome(site) == "" {
			continue
		}
		path, _ := s.nginxPaths(site.Domain)
		old, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return recoverChanges(err)
		}
		if err = write(path, []byte(injectTrafficLogging(string(old), site))); err != nil {
			return recoverChanges(err)
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if _, err := runCommand(ctx, 15*time.Second, "nginx", "-t"); err != nil {
		return recoverChanges(fmt.Errorf("analytics Nginx validation: %w", err))
	}
	if _, err := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx"); err != nil {
		err = recoverChanges(fmt.Errorf("analytics Nginx reload: %w", err))
		_, recoveryErr := runCommand(ctx, 15*time.Second, "systemctl", "reload", "nginx")
		if recoveryErr != nil {
			return fmt.Errorf("%v; previous configuration reload: %w", err, recoveryErr)
		}
		return err
	}
	return nil
}

type trafficRaw struct {
	ID           string `json:"id"`
	Time         string `json:"time"`
	Path         string `json:"path"`
	Method       string `json:"method"`
	Status       string `json:"status"`
	Bytes        string `json:"bytes"`
	Duration     string `json:"duration"`
	Referrer     string `json:"referrer"`
	Agent        string `json:"agent"`
	Address      string `json:"address"`
	ContentType  string `json:"contentType"`
	Protocol     string `json:"protocol"`
	RequestBytes string `json:"requestBytes"`
	Cache        string `json:"cache"`
}

type trafficEvent struct {
	Time         time.Time `json:"time"`
	Path         string    `json:"path"`
	Method       string    `json:"method"`
	Status       int       `json:"status"`
	Bytes        int64     `json:"bytes"`
	RequestBytes int64     `json:"requestBytes"`
	Duration     float64   `json:"duration"`
	Referrer     string    `json:"referrer"`
	Device       string    `json:"device"`
	Browser      string    `json:"browser"`
	OS           string    `json:"os"`
	Bot          string    `json:"bot"`
	Address      string    `json:"address"`
	Country      string    `json:"country"`
	Protocol     string    `json:"protocol"`
	Cache        string    `json:"cache"`
	Pageview     bool      `json:"pageview"`
	Download     bool      `json:"download"`
}

type trafficMetric struct {
	Requests     int64   `json:"requests"`
	Pageviews    int64   `json:"pageviews"`
	Bytes        int64   `json:"bytes"`
	RequestBytes int64   `json:"requestBytes"`
	Errors       int64   `json:"errors"`
	Bots         int64   `json:"bots"`
	Duration     float64 `json:"duration"`
	MaxDuration  float64 `json:"maxDuration"`
}

func (m *trafficMetric) add(n trafficMetric) {
	m.Requests += n.Requests
	m.Pageviews += n.Pageviews
	m.Bytes += n.Bytes
	m.RequestBytes += n.RequestBytes
	m.Errors += n.Errors
	m.Bots += n.Bots
	m.Duration += n.Duration
	if n.MaxDuration > m.MaxDuration {
		m.MaxDuration = n.MaxDuration
	}
}

type trafficDay struct {
	Hours      map[string]*trafficMetric            `json:"hours"`
	Dimensions map[string]map[string]*trafficMetric `json:"dimensions"`
}

type trafficState struct {
	Started        time.Time              `json:"started"`
	Updated        time.Time              `json:"updated"`
	Offsets        map[string]int64       `json:"offsets"`
	CompressedDone map[string]bool        `json:"compressedDone"`
	Checksums      map[string]uint32      `json:"checksums"`
	Days           map[string]*trafficDay `json:"days"`
	Live           []trafficEvent         `json:"live"`
	Malformed      int64                  `json:"malformed"`
	Warning        string                 `json:"warning"`
}

type trafficService struct {
	mu           sync.Mutex
	setupWarning string
	problems     map[string]string
	next         int
	geo          trafficGeo
	geoLoaded    time.Time
	observed     map[string]string
	liveSite     string
	liveStamp    string
	liveCache    *trafficState
}

func newTrafficState() *trafficState {
	return &trafficState{Started: time.Now().UTC(), Offsets: map[string]int64{}, CompressedDone: map[string]bool{}, Checksums: map[string]uint32{}, Days: map[string]*trafficDay{}, Live: []trafficEvent{}}
}

func trafficStatePath(dataDir, siteID string) string {
	sum := sha256.Sum256([]byte(siteID))
	return filepath.Join(dataDir, "website-traffic", hex.EncodeToString(sum[:])+".json")
}

func readTrafficState(path string) (*trafficState, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return newTrafficState(), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > trafficMaxState {
		return nil, errors.New("analytics history exceeded the 16 MiB per-website safety limit; existing history retained")
	}
	s := &trafficState{}
	if err = json.NewDecoder(io.LimitReader(f, trafficMaxState+1)).Decode(s); err != nil {
		return nil, err
	}
	if s.Offsets == nil || s.Days == nil || s.Started.IsZero() || len(s.Days) > 400 || len(s.Offsets) > 512 || len(s.Live) > 200 {
		return nil, errors.New("analytics history is invalid; existing history retained")
	}
	for date, day := range s.Days {
		if _, err := time.Parse("2006-01-02", date); err != nil || day == nil || day.Hours == nil || day.Dimensions == nil || len(day.Hours) > 24 {
			return nil, errors.New("invalid analytics day; existing history retained")
		}
		for hour, metric := range day.Hours {
			n, err := strconv.Atoi(hour)
			if err != nil || n < 0 || n > 23 || metric == nil {
				return nil, errors.New("invalid analytics hour; existing history retained")
			}
		}
		for _, rows := range day.Dimensions {
			if len(rows) > trafficDimensions+1 {
				return nil, errors.New("invalid analytics detail capacity")
			}
			for _, metric := range rows {
				if metric == nil {
					return nil, errors.New("invalid analytics detail; existing history retained")
				}
			}
		}
	}
	if s.Checksums == nil {
		s.Checksums = map[string]uint32{}
	}
	if s.CompressedDone == nil {
		s.CompressedDone = map[string]bool{}
	}
	return s, nil
}

func trafficClient(agent string) (device, browser, system, bot string) {
	a := strings.ToLower(agent)
	device = "Unknown"
	browser = "Unknown"
	system = "Unknown"
	for _, candidate := range []struct{ find, name string }{{"googlebot", "Googlebot"}, {"bingbot", "Bingbot"}, {"duckduckbot", "DuckDuckBot"}, {"facebookexternalhit", "Facebook crawler"}, {"ahrefsbot", "AhrefsBot"}, {"semrushbot", "SemrushBot"}, {"curl/", "curl"}, {"wget/", "wget"}, {"python-requests", "Python requests"}, {"bot", "Other bot"}, {"spider", "Other bot"}, {"crawler", "Other bot"}} {
		if strings.Contains(a, candidate.find) {
			bot = candidate.name
			break
		}
	}
	if strings.Contains(a, "ipad") || strings.Contains(a, "tablet") || strings.Contains(a, "android") && !strings.Contains(a, "mobile") {
		device = "Tablet"
	} else if strings.Contains(a, "mobile") || strings.Contains(a, "iphone") {
		device = "Mobile"
	} else if strings.Contains(a, "windows") || strings.Contains(a, "macintosh") || strings.Contains(a, "x11") {
		device = "Desktop"
	}
	for _, c := range []struct{ find, name string }{{"edg/", "Edge"}, {"opr/", "Opera"}, {"samsungbrowser/", "Samsung Internet"}, {"chrome/", "Chrome"}, {"crios/", "Chrome"}, {"firefox/", "Firefox"}, {"fxios/", "Firefox"}, {"safari/", "Safari"}} {
		if strings.Contains(a, c.find) {
			browser = c.name
			break
		}
	}
	for _, c := range []struct{ find, name string }{{"iphone", "iOS"}, {"ipad", "iPadOS"}, {"android", "Android"}, {"windows", "Windows"}, {"macintosh", "macOS"}, {"linux", "Linux"}} {
		if strings.Contains(a, c.find) {
			system = c.name
			break
		}
	}
	return
}

func maskTrafficAddress(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return "Unavailable"
	}
	if v4 := ip.To4(); v4 != nil {
		return net.IP(v4.Mask(net.CIDRMask(24, 32))).String() + "/24"
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() + "/48"
}

func cleanTrafficText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func parseTrafficEvent(line []byte) (trafficEvent, error) {
	var raw trafficRaw
	var e trafficEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return e, err
	}
	at, err := time.Parse(time.RFC3339, raw.Time)
	if err != nil {
		return e, err
	}
	status, err := strconv.Atoi(raw.Status)
	if err != nil || status < 100 || status > 599 {
		return e, errors.New("invalid status")
	}
	bytes, err := strconv.ParseInt(raw.Bytes, 10, 64)
	if err != nil || bytes < 0 || bytes > 1<<50 {
		return e, errors.New("invalid bytes")
	}
	duration, err := strconv.ParseFloat(raw.Duration, 64)
	if err != nil || duration < 0 || duration > 86400 {
		return e, errors.New("invalid duration")
	}
	// Reject NaN explicitly; it would poison aggregation and JSON persistence.
	if duration != duration {
		return e, errors.New("invalid duration")
	}
	path := strings.SplitN(raw.Path, "?", 2)[0]
	if !strings.HasPrefix(path, "/") {
		return e, errors.New("invalid path")
	}
	ref := strings.ToLower(cleanTrafficText(raw.Referrer, 253))
	if strings.Contains(ref, "://") {
		if u, err := url.Parse(ref); err == nil {
			ref = u.Hostname()
		} else {
			ref = ""
		}
	}
	if ref != "" && (!strings.ContainsAny(ref, "/?#@ ") && ref != "-") {
		ref = strings.TrimSuffix(ref, ".")
	} else {
		ref = "Direct / Unknown"
	}
	e = trafficEvent{Time: at.UTC(), Path: cleanTrafficText(path, 512), Method: cleanTrafficText(raw.Method, 16), Status: status, Bytes: bytes, Duration: duration, Referrer: ref, Address: maskTrafficAddress(raw.Address), Country: "Unavailable", Protocol: cleanTrafficText(raw.Protocol, 20), Cache: cleanTrafficText(raw.Cache, 20)}
	e.RequestBytes, _ = strconv.ParseInt(raw.RequestBytes, 10, 64)
	if e.RequestBytes < 0 || e.RequestBytes > 1<<50 {
		e.RequestBytes = 0
	}
	e.Device, e.Browser, e.OS, e.Bot = trafficClient(raw.Agent)
	ext := strings.ToLower(filepath.Ext(path))
	e.Download = strings.Contains("|.zip|.gz|.pdf|.exe|.apk|.tar|.7z|.rar|.csv|.doc|.docx|.xlsx|.iso|", "|"+ext+"|") && ext != "" && status >= 200 && status < 300
	// Pageviews are successful HTML/XHTML GET responses, excluding known bots
	// and common static assets. Redirects, APIs and error pages are not views.
	ct := strings.ToLower(raw.ContentType)
	asset := ext != "" && strings.Contains("|.css|.js|.json|.xml|.jpg|.jpeg|.gif|.png|.svg|.ico|.webp|.woff|.woff2|.ttf|.map|.txt|.mp4|.mp3|", "|"+ext+"|")
	e.Pageview = e.Method == "GET" && status >= 200 && status < 300 && e.Bot == "" && !asset && !e.Download && (strings.HasPrefix(ct, "text/html") || strings.HasPrefix(ct, "application/xhtml+xml")) && path != "/health" && path != "/healthz"
	return e, nil
}

func (s *trafficState) record(e trafficEvent) error {
	day := e.Time.Format("2006-01-02")
	d := s.Days[day]
	if d == nil {
		if len(s.Days) >= 400 {
			return errors.New("analytics reached 400 stored days; existing history retained, collection paused")
		}
		d = &trafficDay{Hours: map[string]*trafficMetric{}, Dimensions: map[string]map[string]*trafficMetric{}}
		s.Days[day] = d
	}
	m := trafficMetric{Requests: 1, Bytes: e.Bytes, RequestBytes: e.RequestBytes, Duration: e.Duration, MaxDuration: e.Duration}
	if e.Pageview {
		m.Pageviews = 1
	}
	if e.Status >= 400 {
		m.Errors = 1
	}
	if e.Bot != "" {
		m.Bots = 1
	}
	hour := e.Time.Format("15")
	if d.Hours[hour] == nil {
		d.Hours[hour] = &trafficMetric{}
	}
	d.Hours[hour].add(m)
	values := map[string]string{"pages": e.Path, "sources": e.Referrer, "devices": e.Device, "browsers": e.Browser, "systems": e.OS, "statuses": strconv.Itoa(e.Status), "protocols": e.Protocol, "countries": e.Country}
	if e.Bot != "" {
		values["bots"] = e.Bot
	}
	if e.Download {
		values["downloads"] = e.Path
	}
	if e.Duration >= 1 {
		values["slow"] = e.Path
	}
	for name, key := range values {
		if key == "" {
			key = "Unknown"
		}
		if d.Dimensions[name] == nil {
			d.Dimensions[name] = map[string]*trafficMetric{}
		}
		rows := d.Dimensions[name]
		if rows[key] == nil {
			if len(rows) >= trafficDimensions {
				key = "Other (bounded detail)"
			}
			if rows[key] == nil {
				rows[key] = &trafficMetric{}
			}
		}
		rows[key].add(m)
	}
	// Keep the newest requests even when an old rotated file is catching up.
	index := sort.Search(len(s.Live), func(i int) bool { return s.Live[i].Time.After(e.Time) })
	s.Live = append(s.Live, trafficEvent{})
	copy(s.Live[index+1:], s.Live[index:])
	s.Live[index] = e
	if len(s.Live) > 200 {
		s.Live = s.Live[len(s.Live)-200:]
	}
	return nil
}

// The optional local database is an administrator-supplied JSON CIDR-to-country
// map. No client addresses are sent to external services. Longest prefix wins.
type trafficGeo map[string]string

func loadTrafficGeo(dataDir string) trafficGeo {
	f, err := os.Open(filepath.Join(dataDir, "website-traffic-geo.json"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var raw map[string]string
	if json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&raw) != nil || len(raw) > 50000 {
		return nil
	}
	result := trafficGeo{}
	for cidr, country := range raw {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil
		}
		result[network.String()] = cleanTrafficText(country, 80)
	}
	return result
}
func (g trafficGeo) country(address string) string {
	ip := net.ParseIP(address)
	if ip == nil || len(g) == 0 {
		return "Unavailable"
	}
	bits := 128
	if ip.To4() != nil {
		ip = ip.To4()
		bits = 32
	}
	for prefix := bits; prefix >= 0; prefix-- {
		key := (&net.IPNet{IP: ip.Mask(net.CIDRMask(prefix, bits)), Mask: net.CIDRMask(prefix, bits)}).String()
		if country := g[key]; country != "" {
			return country
		}
	}
	return "Unavailable"
}

// Rotation identity uses the first complete JSON line, including Nginx's random
// request_id. It survives rename and gzip, and changes on copy-truncate reuse.
// Partial trailing lines are never checkpointed. Compressed catch-up is bounded.
func collectTrafficFile(path string, s *trafficState, budget *int64, geo trafficGeo) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("unsafe analytics log")
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var reader io.Reader = f
	var gz *gzip.Reader
	if strings.HasSuffix(path, ".gz") {
		gz, err = gzip.NewReader(f)
		if err != nil {
			return false, err
		}
		defer gz.Close()
		reader = gz
	}
	br := bufio.NewReaderSize(reader, trafficMaxLine)
	first, err := br.ReadSlice('\n')
	if err == io.EOF {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("analytics first line: %w", err)
	}
	id := fmt.Sprintf("%x", sha256.Sum256(first))
	if gz != nil && s.CompressedDone[id] {
		return false, nil
	}
	offset := s.Offsets[id]
	if gz != nil && offset > 0 && info.Size() >= 8 {
		var trailer [8]byte
		if _, err := f.ReadAt(trailer[:], info.Size()-8); err == nil && binary.LittleEndian.Uint32(trailer[4:]) == uint32(offset) && binary.LittleEndian.Uint32(trailer[:4]) == s.Checksums[id] {
			// Gzip's size and CRC match the bytes already checkpointed from
			// the uncompressed log. Avoid replaying large completed rotations.
			s.CompressedDone[id] = true
			return true, nil
		}
	}
	if offset < 0 {
		return false, errors.New("invalid analytics checkpoint")
	}
	if offset == 0 && len(s.Offsets) >= 512 {
		return false, errors.New("analytics checkpoint capacity reached; existing history retained")
	}
	if gz == nil {
		if offset > info.Size() {
			return false, errors.New("analytics log truncated without a new identity; review retained history")
		}
		if _, err = f.Seek(offset, io.SeekStart); err != nil {
			return false, err
		}
		br.Reset(f)
	} else {
		// Replay only to the saved offset; cap decompression work per cycle.
		if offset > 64<<20 {
			return false, errors.New("compressed analytics log requires more than 64 MiB replay; retained for administrator review")
		}
		if offset == 0 {
			if err = gz.Close(); err != nil {
				return false, err
			}
			if _, err = f.Seek(0, io.SeekStart); err != nil {
				return false, err
			}
			gz, err = gzip.NewReader(f)
			if err != nil {
				return false, err
			}
			defer gz.Close()
			br.Reset(gz)
		} else {
			remaining := offset - int64(len(first))
			if remaining < 0 {
				return false, errors.New("invalid compressed checkpoint")
			}
			if _, err = io.CopyN(io.Discard, br, remaining); err != nil {
				return false, err
			}
		}
	}
	changed := false
	for *budget > 0 {
		line, readErr := br.ReadSlice('\n')
		if readErr == io.EOF {
			if gz != nil && len(line) == 0 {
				s.CompressedDone[id] = true
				changed = true
			}
			break
		}
		if readErr != nil {
			return changed, fmt.Errorf("analytics log line exceeds %d bytes or cannot be read: %w", trafficMaxLine, readErr)
		}
		if int64(len(line)) > *budget {
			*budget = 0
			break
		}
		e, parseErr := parseTrafficEvent(line)
		if parseErr != nil {
			s.Malformed++
		} else {
			// Do not retain the raw IP or raw User-Agent in persisted snapshots.
			if len(geo) > 0 {
				var raw trafficRaw
				_ = json.Unmarshal(line, &raw)
				e.Country = geo.country(raw.Address)
			}
			if err = s.record(e); err != nil {
				return changed, err
			}
		}
		offset += int64(len(line))
		s.Checksums[id] = crc32.Update(s.Checksums[id], crc32.IEEETable, line)
		s.Offsets[id] = offset
		*budget -= int64(len(line))
		changed = true
	}
	return changed, nil
}

func (a *App) startWebsiteTraffic() {
	a.traffic = &trafficService{problems: map[string]string{}, observed: map[string]string{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	if err := a.system.reconcileTrafficLogging(ctx, a.store.Sites()); err != nil {
		a.traffic.setupWarning = err.Error()
	}
	cancel()
	go func() {
		a.collectWebsiteTraffic()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.collectWebsiteTraffic()
		}
	}()
}

func (a *App) collectWebsiteTraffic() {
	service := a.traffic
	if service == nil || a.system.demo {
		return
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	sites := a.store.Sites()
	if len(sites) == 0 {
		return
	}
	if time.Since(service.geoLoaded) > time.Minute {
		service.geo = loadTrafficGeo(a.system.dataDir)
		service.geoLoaded = time.Now()
	}
	geo := service.geo
	// Round-robin budgets prevent a busy website starving other collectors.
	count := len(sites)
	if count > 32 {
		count = 32
	}
	for n := 0; n < count; n++ {
		site := sites[(service.next+n)%len(sites)]
		if home := siteHome(site); home != "" {
			statePath := trafficStatePath(a.system.dataDir, site.ID)
			paths, err := filepath.Glob(filepath.Join(home, "logs", "traffic-analytics.log*"))
			if err != nil {
				service.problems[site.ID] = err.Error()
				continue
			}
			// Oldest first, then the active file. Keep the existing rotation policy.
			sort.Slice(paths, func(i, j int) bool {
				a, _ := os.Stat(paths[i])
				b, _ := os.Stat(paths[j])
				if a == nil || b == nil {
					return paths[i] < paths[j]
				}
				return a.ModTime().Before(b.ModTime())
			})
			if len(paths) > 32 {
				paths = paths[len(paths)-32:]
			}
			var signature strings.Builder
			for _, path := range paths {
				if info, err := os.Lstat(path); err == nil {
					fmt.Fprintf(&signature, "%s:%d:%d;", path, info.Size(), info.ModTime().UnixNano())
				}
			}
			if len(paths) > 0 && service.observed[site.ID] == signature.String() {
				continue
			}
			s, err := readTrafficState(statePath)
			if err != nil {
				service.problems[site.ID] = "Cannot read retained analytics history: " + err.Error()
				continue
			}
			budget := int64(trafficReadBudget)
			changed := false
			problem := ""
			for _, path := range paths {
				if budget <= 0 {
					break
				}
				did, collectErr := collectTrafficFile(path, s, &budget, geo)
				changed = changed || did
				if collectErr != nil {
					problem = collectErr.Error()
				}
			}
			if len(paths) == 0 {
				problem = "Waiting for Nginx analytics logs. Logging starts after the configuration is installed and requests reach this website."
			}
			if budget <= 0 && problem == "" {
				problem = "Collection is catching up within its per-website processing budget."
			}
			service.problems[site.ID] = problem
			if changed {
				sort.SliceStable(s.Live, func(i, j int) bool { return s.Live[i].Time.Before(s.Live[j].Time) })
				s.Updated = time.Now().UTC()
				s.Warning = problem
				data, encodeErr := json.Marshal(s)
				if encodeErr == nil && len(data) > trafficMaxState {
					encodeErr = errors.New("analytics history exceeded 16 MiB; existing history and checkpoint retained")
				}
				if encodeErr == nil {
					encodeErr = os.MkdirAll(filepath.Dir(statePath), 0700)
				}
				if encodeErr == nil {
					encodeErr = writeAtomic(statePath, data, 0600)
				}
				if encodeErr != nil {
					service.problems[site.ID] = "Cannot save analytics: " + encodeErr.Error()
				}
			}
			if service.problems[site.ID] == "" && budget > 0 {
				if service.observed == nil {
					service.observed = map[string]string{}
				}
				service.observed[site.ID] = signature.String()
			}
		}
	}
	service.next = (service.next + count) % len(sites)
}

func (a *App) websiteTraffic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	site := a.store.SiteByID(r.URL.Query().Get("siteId"))
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	if a.traffic == nil {
		writeError(w, 503, "Website traffic collector is not started")
		return
	}
	start, end, err := trafficRange(r.URL.Query(), time.Now().UTC())
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	a.traffic.mu.Lock()
	statePath := trafficStatePath(a.system.dataDir, site.ID)
	var s *trafficState
	if r.URL.Query().Get("view") == "live" {
		stamp := ""
		if info, statErr := os.Stat(statePath); statErr == nil {
			stamp = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
		}
		if a.traffic.liveSite == site.ID && a.traffic.liveStamp == stamp && a.traffic.liveCache != nil {
			s = a.traffic.liveCache
		} else {
			s, err = readTrafficState(statePath)
			if err == nil {
				cached := *s
				cached.Days = nil
				cached.Offsets = nil
				cached.CompressedDone = nil
				cached.Checksums = nil
				a.traffic.liveCache = &cached
				a.traffic.liveSite = site.ID
				a.traffic.liveStamp = stamp
			}
		}
	} else {
		s, err = readTrafficState(statePath)
	}
	warning := a.traffic.setupWarning
	_, hasCurrentCheck := a.traffic.problems[site.ID]
	if p := a.traffic.problems[site.ID]; p != "" {
		if warning != "" {
			warning += "; "
		}
		warning += p
	}
	a.traffic.mu.Unlock()
	if err != nil {
		writeError(w, 503, "Cannot read retained analytics history: "+err.Error())
		return
	}
	if siteHome(*site) == "" {
		warning = "Analytics logging is unavailable for this legacy website root; migrate it to a managed website home."
	}
	if warning == "" && !hasCurrentCheck {
		warning = s.Warning
	}
	if r.URL.Query().Get("view") == "live" {
		entries := []trafficEvent{}
		filter := strings.ToLower(r.URL.Query().Get("filter"))
		code := r.URL.Query().Get("status")
		for i := len(s.Live) - 1; i >= 0; i-- {
			e := s.Live[i]
			if !e.Time.Before(start) && e.Time.Before(end) && (filter == "" || strings.Contains(strings.ToLower(e.Path+" "+e.Referrer+" "+e.Browser+" "+e.Bot), filter)) && (code == "" || strconv.Itoa(e.Status) == code) {
				entries = append(entries, e)
			}
		}
		writeJSON(w, 200, map[string]any{"entries": entries, "updated": s.Updated, "warning": warning, "limit": 200, "malformed": s.Malformed})
		return
	}
	report := buildTrafficReport(s, start, end, r.URL.Query())
	report["warning"] = warning
	report["updated"] = s.Updated
	report["started"] = s.Started
	report["malformed"] = s.Malformed
	report["geoAvailable"] = len(loadTrafficGeo(a.system.dataDir)) > 0
	writeJSON(w, 200, report)
}

func trafficRange(q url.Values, now time.Time) (time.Time, time.Time, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -6)
	end := today.AddDate(0, 0, 1)
	switch q.Get("range") {
	case "today":
		start = today
	case "30":
		start = today.AddDate(0, 0, -29)
	case "custom":
		var err error
		start, err = time.Parse("2006-01-02", q.Get("from"))
		if err != nil {
			return start, end, errors.New("invalid start date")
		}
		end, err = time.Parse("2006-01-02", q.Get("to"))
		if err != nil {
			return start, end, errors.New("invalid end date")
		}
		end = end.AddDate(0, 0, 1)
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour || end.After(today.AddDate(0, 0, 1)) {
		return start, end, errors.New("select a date range of at most 366 days, ending no later than today (UTC)")
	}
	if q.Get("group") == "hourly" && end.Sub(start) > 31*24*time.Hour {
		return start, end, errors.New("hourly charts support at most 31 days; select daily, weekly or monthly for longer ranges")
	}
	return start, end, nil
}

type trafficRow struct {
	Name string `json:"name"`
	trafficMetric
}
type trafficPoint struct {
	Time string `json:"time"`
	trafficMetric
}

func buildTrafficReport(s *trafficState, start, end time.Time, q url.Values) map[string]any {
	metric := trafficMetric{}
	points := map[string]*trafficMetric{}
	rows := map[string]*trafficMetric{}
	group := q.Get("group")
	if group != "hourly" && group != "weekly" && group != "monthly" {
		group = "daily"
	}
	dimension := q.Get("dimension")
	if !strings.Contains("|pages|sources|devices|browsers|systems|bots|statuses|downloads|slow|protocols|countries|", "|"+dimension+"|") || dimension == "" {
		dimension = "pages"
	}
	for date, day := range s.Days {
		at, err := time.Parse("2006-01-02", date)
		if err != nil || at.Before(start) || !at.Before(end) {
			continue
		}
		for hour, value := range day.Hours {
			metric.add(*value)
			key := date
			switch group {
			case "hourly":
				key = date + "T" + hour + ":00:00Z"
			case "weekly":
				delta := (int(at.Weekday()) + 6) % 7
				key = at.AddDate(0, 0, -delta).Format("2006-01-02")
			case "monthly":
				key = at.Format("2006-01")
			}
			if points[key] == nil {
				points[key] = &trafficMetric{}
			}
			points[key].add(*value)
		}
		for key, value := range day.Dimensions[dimension] {
			if rows[key] == nil {
				rows[key] = &trafficMetric{}
			}
			rows[key].add(*value)
		}
	}
	chart := []trafficPoint{}
	for key, value := range points {
		chart = append(chart, trafficPoint{key, *value})
	}
	sort.Slice(chart, func(i, j int) bool { return chart[i].Time < chart[j].Time })
	list := []trafficRow{}
	for key, value := range rows {
		list = append(list, trafficRow{key, *value})
	}
	sort.Slice(list, func(i, j int) bool {
		if (dimension == "pages" || dimension == "sources") && list[i].Pageviews != list[j].Pageviews {
			return list[i].Pageviews > list[j].Pageviews
		}
		if dimension == "slow" && list[i].MaxDuration != list[j].MaxDuration {
			return list[i].MaxDuration > list[j].MaxDuration
		}
		if list[i].Requests == list[j].Requests {
			return list[i].Name < list[j].Name
		}
		return list[i].Requests > list[j].Requests
	})
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("size"))
	if size != 25 && size != 50 && size != 100 {
		size = 10
	}
	pages := (len(list) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	first := (page - 1) * size
	last := first + size
	if last > len(list) {
		last = len(list)
	}
	return map[string]any{"summary": metric, "points": chart, "rows": list[first:last], "total": len(list), "page": page, "pages": pages, "size": size, "dimension": dimension, "group": group, "timezone": "UTC", "from": start, "to": end, "geoAvailable": false}
}
