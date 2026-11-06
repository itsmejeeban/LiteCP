package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const captchaAddress = "127.0.0.1:9092"
const captchaRuntimeFile = "/var/lib/litecp-captcha/config.json"
const captchaPrefix = "/_litecp_captcha/"

type captchaRuntime struct {
	Key   string                        `json:"key"`
	Sites map[string]captchaRuntimeSite `json:"sites"`
}
type captchaRuntimeSite struct {
	Domain string        `json:"domain"`
	Policy CaptchaPolicy `json:"policy"`
}
type captchaChallenge struct {
	Site, Domain, Return, Binding, Digest string
	Expires                               time.Time
}
type captchaCounters struct {
	Checked, Challenged, Passed, Failed, Limited uint64
	Events                                       []string
}
type captchaGateway struct {
	slots      chan struct{}
	mu         sync.Mutex
	runtime    captchaRuntime
	loaded     time.Time
	challenges map[string]captchaChallenge
	buckets    map[string]loginAttempt
	stats      map[string]*captchaCounters
	now        func() time.Time
}

func newCaptchaGateway() *captchaGateway {
	return &captchaGateway{slots: make(chan struct{}, 32), challenges: map[string]captchaChallenge{}, buckets: map[string]loginAttempt{}, stats: map[string]*captchaCounters{}, now: time.Now}
}
func captchaMAC(key, value string) string {
	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func captchaBinding(r *http.Request) string {
	h := sha256.Sum256([]byte(r.Header.Get("X-LiteCP-IP") + "\x00" + r.UserAgent()))
	return hex.EncodeToString(h[:16])
}
func safeCaptchaReturn(value string) string {
	u, err := url.Parse(value)
	if err != nil || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n\x00") || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, captchaPrefix) || len(value) > 2048 {
		return "/"
	}
	return value
}
func (g *captchaGateway) reload() error {
	if g.now().Sub(g.loaded) < time.Second {
		return nil
	}
	data, err := os.ReadFile(captchaRuntimeFile)
	if err != nil {
		return err
	}
	var cfg captchaRuntime
	if json.Unmarshal(data, &cfg) != nil || len(cfg.Key) < 32 {
		return fmt.Errorf("invalid CAPTCHA runtime configuration")
	}
	g.runtime = cfg
	g.loaded = g.now()
	return nil
}
func (g *captchaGateway) limit(key string, maximum int) bool {
	return g.limitWindow(key, maximum, time.Minute)
}
func (g *captchaGateway) limitWindow(key string, maximum int, window time.Duration) bool {
	now := g.now()
	bucket := g.buckets[key]
	if now.Sub(bucket.Since) >= window {
		bucket = loginAttempt{Since: now}
	}
	if bucket.Count >= maximum {
		return false
	}
	if len(g.buckets) >= 8192 {
		for k, b := range g.buckets {
			if now.Sub(b.Since) >= time.Minute {
				delete(g.buckets, k)
			}
		}
		if _, ok := g.buckets[key]; !ok && len(g.buckets) >= 8192 {
			return false
		}
	}
	bucket.Count++
	g.buckets[key] = bucket
	return true
}
func (g *captchaGateway) event(site, message string) {
	s := g.stats[site]
	s.Events = append(s.Events, g.now().UTC().Format(time.RFC3339)+" "+message)
	if len(s.Events) > 100 {
		s.Events = s.Events[len(s.Events)-100:]
	}
}
func (g *captchaGateway) clearance(site string, record captchaRuntimeSite, binding string, expiry int64) string {
	payload := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%d", site, record.Domain, record.Policy.Revision, binding, expiry)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + captchaMAC(g.runtime.Key, encoded)
}
func (g *captchaGateway) verified(r *http.Request, site string, record captchaRuntimeSite) bool {
	cookie, err := r.Cookie("__Host-litecp_clearance")
	if err != nil || len(cookie.Value) > 1024 {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(captchaMAC(g.runtime.Key, parts[0]))) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	fields := strings.Split(string(data), "\x00")
	if len(fields) != 5 {
		return false
	}
	var expiry int64
	if _, err = fmt.Sscan(fields[4], &expiry); err != nil || expiry <= g.now().Unix() || expiry > g.now().Add(time.Duration(record.Policy.ClearanceMinutes)*time.Minute).Unix()+10 {
		return false
	}
	return cookie.Value == g.clearance(site, record, captchaBinding(r), expiry)
}

var captchaPage = template.Must(template.New("challenge").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>Verify to continue</title>
<style>
*{box-sizing:border-box}body{margin:0;min-height:100vh;min-height:100svh;padding:24px;display:grid;place-items:center;font:16px system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#f8fafc;background:radial-gradient(ellipse at 15% 15%,#263c75 0,transparent 48%),radial-gradient(ellipse at 90% 85%,#36265c 0,transparent 48%),linear-gradient(145deg,#101827,#0b101c 65%)}main{width:100%;max-width:420px;padding:40px 28px;text-align:center;border:1px solid #ffffff24;border-radius:24px;background:#111827e8;box-shadow:0 24px 80px #0005}h1{margin:0;font-size:clamp(25px,5vw,30px);font-weight:650;letter-spacing:-.7px}.domain{margin:12px 0 28px;color:#b9c6df;font-size:14px;overflow-wrap:anywhere}img{display:block;width:264px;max-width:100%;height:auto;margin:0 auto 24px;border-radius:12px}form{display:flex;flex-direction:column;align-items:center;gap:18px}input,button{font:inherit;border-radius:12px}input[name=answer]{width:100%;max-width:264px;min-height:54px;border:1px solid #69758c;background:#0c1424;color:#fff;text-align:center;font-size:26px;letter-spacing:8px;padding:10px 8px 10px 16px}input:focus-visible,button:focus-visible,a:focus-visible{outline:3px solid #93c5fd;outline-offset:4px}button{width:100%;max-width:264px;min-height:50px;padding:12px 18px;border:1px solid #6b9bff;background:linear-gradient(135deg,#3b82f6,#2563eb);color:#fff;font-weight:600;cursor:pointer;box-shadow:0 6px 24px #2563eb30}button:hover{background:#1d4ed8}.retry{display:inline-block;margin-top:24px;color:#b9d4ff;font-size:14px;text-underline-offset:4px}.retry:hover{color:#fff}@media(max-width:360px){body{padding:16px}main{padding:32px 20px}}
</style></head><body><main><h1>Verify to continue</h1><p class="domain">{{.Domain}}</p><img src="/_litecp_captcha/image?id={{.ID}}" width="264" height="84" alt="CAPTCHA image containing six digits"><form method="post" action="/_litecp_captcha/verify"><input type="hidden" name="id" value="{{.ID}}"><input id="answer" name="answer" aria-label="Verification digits" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" required autocomplete="off"><button type="submit">Verify and Open</button></form><a class="retry" href="{{.Retry}}">Get a new challenge</a></main></body></html>`))

func (g *captchaGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		http.Error(w, "Verification service busy", 503)
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	if err := g.reload(); err != nil {
		http.Error(w, "Verification service unavailable", 503)
		return
	}
	if r.URL.Path == "/metrics" && r.Header.Get("X-LiteCP-Mode") == "metrics" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+g.runtime.Key)) != 1 {
			http.Error(w, "Forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(g.stats)
		return
	}
	site := r.Header.Get("X-LiteCP-Site")
	record, ok := g.runtime.Sites[site]
	if !ok || record.Policy.Mode == "off" {
		http.Error(w, "Unknown protected website", 403)
		return
	}
	if r.Header.Get("X-LiteCP-Host") != record.Domain {
		http.Error(w, "Website hostname mismatch", 403)
		return
	}
	if g.stats[site] == nil {
		if len(g.stats) >= 2048 {
			http.Error(w, "Service capacity reached", 503)
			return
		}
		g.stats[site] = &captchaCounters{}
	}
	stats := g.stats[site]
	mode := r.Header.Get("X-LiteCP-Mode")
	original := r.Header.Get("X-LiteCP-URI")
	path := strings.SplitN(original, "?", 2)[0]
	if mode == "check" {
		stats.Checked++
		if strings.HasPrefix(path, "/.well-known/acme-challenge/") {
			w.WriteHeader(204)
			return
		}
		ipOK := g.limitWindow("rps:ip:"+site+":"+r.Header.Get("X-LiteCP-IP"), record.Policy.RequestsPerSecond, time.Second)
		siteOK := g.limitWindow("rps:site:"+site, record.Policy.SiteRequestsPerSecond, time.Second)
		if !ipOK || !siteOK {
			stats.Limited++
			if record.Policy.Mode != "monitor" {
				// auth_request accepts only 2xx, 401 and 403. Route through the
				// named proxy handler to return a real 429 to the visitor.
				w.Header().Set("X-LiteCP-Captcha-Limited", "1")
				w.WriteHeader(401)
				return
			}
		}
		if record.Policy.Mode == "monitor" {
			w.WriteHeader(204)
			return
		}
		for _, prefix := range record.Policy.Exclusions {
			if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
				w.WriteHeader(204)
				return
			}
		}
		if g.verified(r, site, record) {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(401)
		}
		return
	}
	if r.Header.Get("X-LiteCP-Scheme") != "https" {
		http.Error(w, "HTTPS is required for website verification", 403)
		return
	}
	if mode != "public" && mode != "challenge" {
		http.Error(w, "Forbidden", 403)
		return
	}
	if mode == "challenge" && r.Header.Get("X-LiteCP-Limited") == "1" {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Website request limit reached. Retry in one second.", 429)
		return
	}
	if !g.limit("global", 1200) || !g.limit(site+":total", record.Policy.ChallengesPerMinute) || !g.limit(site+":"+r.Header.Get("X-LiteCP-IP"), record.Policy.AttemptsPerMinute) {
		stats.Limited++
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Too many verification requests. Retry in one minute.", 429)
		return
	}
	binding := captchaBinding(r)
	if mode == "public" && r.URL.Path == captchaPrefix+"image" {
		challenge, ok := g.challenges[r.URL.Query().Get("id")]
		if !ok || challenge.Site != site || challenge.Binding != binding || !challenge.Expires.After(g.now()) {
			http.Error(w, "Challenge expired. Refresh verification.", 410)
			return
		}
		digits := captchaDigits(g.runtime.Key, r.URL.Query().Get("id"))
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, captchaImage(digits))
		return
	}
	if mode == "public" && r.URL.Path == captchaPrefix+"verify" {
		if r.Method != "POST" {
			http.Error(w, "Use POST", 405)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid verification request", 400)
			return
		}
		id := r.PostForm.Get("id")
		challenge, ok := g.challenges[id]
		if !ok || challenge.Site != site || challenge.Domain != record.Domain || challenge.Binding != binding || !challenge.Expires.After(g.now()) {
			stats.Failed++
			http.Error(w, "Challenge expired or already used. Return to the website and retry.", 403)
			return
		}
		delete(g.challenges, id)
		digest := captchaMAC(g.runtime.Key, id+":"+r.PostForm.Get("answer"))
		if !hmac.Equal([]byte(challenge.Digest), []byte(digest)) {
			stats.Failed++
			g.event(site, "CAPTCHA answer rejected")
			http.Error(w, "Incorrect answer. Return to the website for a new challenge.", 403)
			return
		}
		stats.Passed++
		g.event(site, "Visitor verified")
		expiry := g.now().Add(time.Duration(record.Policy.ClearanceMinutes) * time.Minute)
		http.SetCookie(w, &http.Cookie{Name: "__Host-litecp_clearance", Value: g.clearance(site, record, binding, expiry.Unix()), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: record.Policy.ClearanceMinutes * 60, Expires: expiry})
		http.Redirect(w, r, challenge.Return, 303)
		return
	}
	if mode == "public" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("X-LiteCP-Method") != "GET" && r.Header.Get("X-LiteCP-Method") != "HEAD" {
		http.Error(w, "Verify this website in your browser before submitting requests. This request was not replayed.", 403)
		return
	}
	for id, challenge := range g.challenges {
		if !challenge.Expires.After(g.now()) {
			delete(g.challenges, id)
		}
	}
	if len(g.challenges) >= 4096 {
		http.Error(w, "Verification capacity reached. Retry shortly.", 503)
		return
	}
	id, err := randomToken(24)
	if err != nil {
		http.Error(w, "Challenge unavailable", 503)
		return
	}
	digits := captchaDigits(g.runtime.Key, id)
	target := safeCaptchaReturn(original)
	g.challenges[id] = captchaChallenge{Site: site, Domain: record.Domain, Return: target, Binding: binding, Digest: captchaMAC(g.runtime.Key, id+":"+digits), Expires: g.now().Add(3 * time.Minute)}
	stats.Challenged++
	g.event(site, "Verification challenge issued")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(403)
	_ = captchaPage.Execute(w, map[string]string{"Domain": record.Domain, "ID": id, "Retry": target})
}
func captchaDigits(key, id string) string {
	value := captchaMAC(key, id)
	digits := make([]byte, 6)
	for i := range digits {
		digits[i] = '0' + value[i]%10
	}
	return string(digits)
}

var captchaFont = [10][7]string{
	{"01110", "10001", "10011", "10101", "11001", "10001", "01110"}, {"00100", "01100", "00100", "00100", "00100", "00100", "01110"}, {"01110", "10001", "00001", "00010", "00100", "01000", "11111"}, {"11110", "00001", "00001", "01110", "00001", "00001", "11110"}, {"00010", "00110", "01010", "10010", "11111", "00010", "00010"}, {"11111", "10000", "10000", "11110", "00001", "00001", "11110"}, {"01110", "10000", "10000", "11110", "10001", "10001", "01110"}, {"11111", "00001", "00010", "00100", "01000", "01000", "01000"}, {"01110", "10001", "10001", "01110", "10001", "10001", "01110"}, {"01110", "10001", "10001", "01111", "00001", "00001", "01110"},
}

func captchaImage(digits string) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 264, 84))
	for y := 0; y < 84; y++ {
		for x := 0; x < 264; x++ {
			img.Set(x, y, color.RGBA{240, 244, 250, 255})
		}
	}
	noise := make([]byte, 768)
	_, _ = io.ReadFull(rand.Reader, noise)
	for i := 0; i < len(noise)-2; i += 3 {
		img.Set(int(noise[i])%264, int(noise[i+1])%84, color.RGBA{150, 165, 190, 255})
	}
	for index, d := range digits {
		if d < '0' || d > '9' {
			continue
		}
		offset := int(noise[index]) % 9
		for y, row := range captchaFont[d-'0'] {
			for x, pixel := range row {
				if pixel == '1' {
					for yy := 0; yy < 7; yy++ {
						for xx := 0; xx < 5; xx++ {
							img.Set(16+index*40+x*5+xx, 12+offset+y*7+yy, color.RGBA{25, 40, 80, 255})
						}
					}
				}
			}
		}
	}
	return img
}
func runCaptchaGateway() error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("CAPTCHA gateway must run as its dedicated unprivileged account")
	}
	gateway := newCaptchaGateway()
	server := &http.Server{Addr: captchaAddress, Handler: gateway, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10}
	return server.ListenAndServe()
}
