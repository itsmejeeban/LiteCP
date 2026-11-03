package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

var urlUploadSlots = make(chan struct{}, 2)

func validateUploadURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return nil, errors.New("use a public HTTP or HTTPS file URL without embedded credentials or a fragment")
	}
	if p := u.Port(); p != "" && !((u.Scheme == "http" && p == "80") || (u.Scheme == "https" && p == "443")) {
		return nil, errors.New("only standard HTTP and HTTPS ports are supported")
	}
	return u, nil
}

func publicDownloadIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Only globally allocated IPv6 unicast is eligible. Translation and tunnel
	// prefixes can otherwise relay an apparently public URL to an internal IPv4
	// address on hosts with NAT64 or transition routing configured.
	if ip.To4() == nil {
		_, globalIPv6, _ := net.ParseCIDR("2000::/3")
		if !globalIPv6.Contains(ip) {
			return false
		}
	}
	for _, network := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "3fff::/20", "64:ff9b::/96", "2002::/16"} {
		_, block, _ := net.ParseCIDR(network)
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

func urlUploadTransport() *http.Transport {
	return &http.Transport{Proxy: nil, DisableCompression: true, ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 15 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("could not resolve download host")
		}
		for _, ip := range ips {
			if !publicDownloadIP(ip.IP) {
				return nil, errors.New("private or reserved download addresses are blocked")
			}
		}
		dialer := net.Dialer{Timeout: 15 * time.Second}
		var last error
		for _, ip := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
			last = e
		}
		return nil, last
	}}
}

type urlUploadReader struct {
	ctx    context.Context
	source io.Reader
	bytes  int64
	report func(int64)
	last   time.Time
}

func (p *urlUploadReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.source.Read(b)
	p.bytes += int64(n)
	if time.Since(p.last) > 250*time.Millisecond {
		p.report(p.bytes)
		p.last = time.Now()
	}
	return n, err
}

func (a *App) uploadFileURL(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SiteID string `json:"siteId"`
		Path   string `json:"path"`
		URL    string `json:"url"`
		Name   string `json:"name"`
	}
	if decodeJSONWithLimit(r, &input, 16<<10) != nil {
		writeError(w, 400, "Invalid download request")
		return
	}
	site := a.store.SiteByID(input.SiteID)
	if site == nil {
		writeError(w, 404, "Website not found")
		return
	}
	if a.system.demo {
		writeError(w, 400, "URL uploads require the installed server")
		return
	}
	u, err := validateUploadURL(strings.TrimSpace(input.URL))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = path.Base(u.Path)
	}
	if err := validateFileName(name); err != nil {
		writeError(w, 400, "Enter a valid destination filename")
		return
	}
	if _, _, err := resolveManagedPath(managedSiteRoot(*site), input.Path, false); err != nil {
		writeError(w, 400, "Invalid destination directory")
		return
	}
	exists, err := a.system.ManagedPathExists(*site, path.Join(input.Path, name))
	if err != nil {
		writeError(w, 400, "Cannot access destination")
		return
	}
	if exists {
		writeError(w, 409, "Destination already exists; choose another filename")
		return
	}
	select {
	case urlUploadSlots <- struct{}{}:
		defer func() { <-urlUploadSlots }()
	default:
		writeError(w, 429, "Two URL downloads are already running; try again later")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	transport := urlUploadTransport()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if _, err := validateUploadURL(req.URL.String()); err != nil {
			return err
		}
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade is blocked")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeError(w, 400, "Invalid URL")
		return
	}
	req.Header.Set("User-Agent", "LiteCP-URL-Upload")
	response, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "Cannot download URL. Check public reachability, redirects and TLS; private addresses are blocked.")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeError(w, 502, fmt.Sprintf("Download server returned HTTP %d", response.StatusCode))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "Download progress streaming unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(value any) {
		if json.NewEncoder(w).Encode(value) != nil {
			cancel()
		}
		flusher.Flush()
	}
	send(map[string]any{"status": "downloading", "bytes": 0, "total": response.ContentLength, "name": name})
	reader := &urlUploadReader{ctx: ctx, source: response.Body, report: func(n int64) {
		send(map[string]any{"status": "downloading", "bytes": n, "total": response.ContentLength})
	}}
	entry, err := a.system.SaveNewUpload(*site, input.Path, name, reader)
	if err != nil {
		send(map[string]string{"status": "error", "error": "Download was interrupted or the destination could not be saved. Check disk space and refresh before retrying."})
		return
	}
	_ = a.store.AddActivity("File downloaded from URL", site.Domain+": "+entry.Path, "success")
	send(map[string]any{"status": "complete", "file": entry, "bytes": reader.bytes})
}
