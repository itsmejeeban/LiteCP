package main

// The broker protocol is deliberately not HTTP, a command runner, or a file
// primitive. It selects a registered control operation, retains that operation's
// authentication/validation, and streams its bounded request/response body.
import (
	"context"
	"crypto/tls"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const brokerSocket = "/run/litecp-broker/control.sock"
const brokerFrameLimit = 64 << 10
const brokerStreamLimit int64 = 64 << 30

// New routes are not implicitly granted broker access. Add an explicit entry
// here only after reviewing the operation's authentication and validation.
//
//go:embed deploy/broker-operations.json
var brokerOperationPolicy []byte
var brokerAllowedOperations = func() map[string]bool {
	var names []string
	if err := json.Unmarshal(brokerOperationPolicy, &names); err != nil {
		panic(err)
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	return allowed
}()

func runtimeRootHTTPForbidden(args []string, demo bool) bool {
	return !demo && os.Geteuid() == 0 && !(len(args) == 2 && args[1] == "broker")
}

type brokerRequest struct {
	TLS           bool        `json:"tls,omitempty"`
	Operation     string      `json:"operation"`
	Suffix        string      `json:"suffix,omitempty"`
	Method        string      `json:"method"`
	Query         string      `json:"query,omitempty"`
	Host          string      `json:"host"`
	RemoteAddr    string      `json:"remoteAddr"`
	Header        http.Header `json:"header"`
	ContentLength int64       `json:"contentLength"`
}
type brokerResponse struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
}
type brokerPeerUIDKey struct{}
type frontendConnectionKey struct{}

func writeBrokerFrame(w io.Writer, data []byte) error {
	if len(data) > brokerFrameLimit {
		return errors.New("broker frame too large")
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	if _, err := w.Write(size[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}
func readBrokerFrame(r io.Reader) ([]byte, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n > brokerFrameLimit {
		return nil, errors.New("broker frame too large")
	}
	data := make([]byte, n)
	_, err := io.ReadFull(r, data)
	return data, err
}
func writeBrokerJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeBrokerFrame(w, data)
}
func readBrokerJSON(r io.Reader, value any) error {
	data, err := readBrokerFrame(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

type brokerBodyReader struct {
	source    io.Reader
	buffered  []byte
	remaining int64
	ended     bool
	onEOF     func()
}

func (r *brokerBodyReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buffered) == 0 {
		if r.ended {
			return 0, io.EOF
		}
		frame, err := readBrokerFrame(r.source)
		if err != nil {
			return 0, err
		}
		if len(frame) == 0 {
			r.ended = true
			if r.onEOF != nil {
				r.onEOF()
				r.onEOF = nil
			}
			return 0, io.EOF
		}
		if int64(len(frame)) > r.remaining {
			return 0, errors.New("broker stream too large")
		}
		r.remaining -= int64(len(frame))
		r.buffered = frame
	}
	n := copy(p, r.buffered)
	r.buffered = r.buffered[n:]
	return n, nil
}

type brokerBodyWriter struct {
	target    io.Writer
	remaining int64
}

func (w *brokerBodyWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("broker stream too large")
	}
	total := 0
	for len(p) > 0 {
		n := len(p)
		if n > 32<<10 {
			n = 32 << 10
		}
		if err := writeBrokerFrame(w.target, p[:n]); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
		w.remaining -= int64(n)
	}
	return total, nil
}

type brokerResponseWriter struct {
	conn   io.Writer
	header http.Header
	sent   bool
	err    error
	body   brokerBodyWriter
}

func (w *brokerResponseWriter) Header() http.Header { return w.header }
func (w *brokerResponseWriter) WriteHeader(status int) {
	if w.sent {
		return
	}
	w.sent = true
	w.err = writeBrokerJSON(w.conn, brokerResponse{status, w.header})
}
func (w *brokerResponseWriter) Write(p []byte) (int, error) {
	if !w.sent {
		w.WriteHeader(200)
	}
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.body.Write(p)
	if err != nil {
		w.err = err
	}
	return n, err
}
func (w *brokerResponseWriter) Flush() {
	if !w.sent {
		w.WriteHeader(200)
	}
}

func brokerOperation(mux *http.ServeMux, path string) (string, string, error) {
	if !strings.HasPrefix(path, "/api/") && path != "/internal/phpmyadmin-ticket" && path != "/internal/webmail-ticket" {
		return "", "", errors.New("unknown broker operation")
	}
	// Root shells cannot be obtained through a separated web service.
	if path == "/api/terminal" || strings.HasPrefix(path, "/api/terminal/") {
		return "", "", errors.New("Browser terminal is unavailable in privilege-separated mode. Use SSH for server shell access.")
	}
	_, pattern := mux.Handler(&http.Request{Method: http.MethodGet, URL: &url.URL{Path: path}})
	if pattern == "" || !brokerAllowedOperations[pattern] || !strings.HasPrefix(path, pattern) {
		return "", "", errors.New("unknown broker operation")
	}
	if path != pattern && !strings.HasSuffix(pattern, "/") {
		return "", "", errors.New("invalid broker operation suffix")
	}
	return pattern, strings.TrimPrefix(path, pattern), nil
}

func serveBrokerRequest(conn net.Conn, app *App, mux *http.ServeMux, expectedUID uint32) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	uid, err := unixPeerUID(conn)
	if err != nil || uid != expectedUID {
		return
	}
	var input brokerRequest
	if err = readBrokerJSON(conn, &input); err != nil {
		return
	}
	path := input.Operation + input.Suffix
	op, suffix, err := brokerOperation(mux, path)
	if err != nil || op != input.Operation || suffix != input.Suffix || strings.ContainsAny(path, "\x00\r\n?#") || len(input.Query) > 16384 || input.ContentLength > brokerStreamLimit || input.ContentLength < -1 {
		return
	}
	if input.Method != "GET" && input.Method != "HEAD" && input.Method != "POST" && input.Method != "PUT" && input.Method != "DELETE" && input.Method != "PATCH" {
		return
	}
	if input.Host == "" || strings.ContainsAny(input.Host, "\x00\r\n/\\") {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(45 * time.Minute))
	if input.Operation == "/api/login" {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), brokerPeerUIDKey{}, uid))
	defer cancel()
	body := &brokerBodyReader{source: conn, remaining: brokerStreamLimit, onEOF: func() { go func() { var extra [1]byte; _, _ = conn.Read(extra[:]); cancel() }() }}
	r := &http.Request{Method: input.Method, URL: &url.URL{Path: path, RawQuery: input.Query}, Host: input.Host, RemoteAddr: input.RemoteAddr, Header: input.Header, ContentLength: input.ContentLength, Body: io.NopCloser(body)}
	if input.TLS {
		r.TLS = &tls.ConnectionState{}
	}
	r = r.WithContext(ctx)
	w := &brokerResponseWriter{conn: conn, header: make(http.Header), body: brokerBodyWriter{target: conn, remaining: brokerStreamLimit}}
	// Disconnects cancel the handler once it has consumed the framed body.
	// Deadlines and operation-specific context timeouts also bound background work.
	app.securityHeaders(mux).ServeHTTP(w, r)
	if !w.sent {
		w.WriteHeader(200)
	}
	if w.err == nil {
		_ = writeBrokerFrame(conn, nil)
	}
}

func forwardBrokerRequest(w http.ResponseWriter, r *http.Request, mux *http.ServeMux, socket string) {
	op, suffix, err := brokerOperation(mux, r.URL.Path)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(r.Context(), "unix", socket)
	if err != nil {
		writeError(w, 503, "The local management broker is unavailable.")
		return
	}
	defer conn.Close()
	if uid, e := unixPeerUID(conn); e != nil || uid != 0 {
		writeError(w, 503, "The local management broker identity could not be verified.")
		return
	}
	_ = conn.SetDeadline(time.Now().Add(45 * time.Minute))
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-r.Context().Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	input := brokerRequest{Operation: op, Suffix: suffix, Method: r.Method, Query: r.URL.RawQuery, Host: r.Host, RemoteAddr: r.RemoteAddr, Header: r.Header.Clone(), ContentLength: r.ContentLength}
	input.TLS = r.TLS != nil
	if err = writeBrokerJSON(conn, input); err != nil {
		writeError(w, 502, "Management request metadata is invalid or too large.")
		return
	}
	// Request uploads and response downloads stream without buffering entire archives.
	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		body := &brokerBodyWriter{target: conn, remaining: brokerStreamLimit}
		if r.Body != nil {
			if _, e := io.Copy(body, r.Body); e != nil {
				// An early rejection must not discard its response when the upload
				// stops. Only close the request direction of this Unix connection.
				if local, ok := conn.(*net.UnixConn); ok {
					_ = local.CloseWrite()
				}
				return
			}
		}
		_ = writeBrokerFrame(conn, nil)
	}()
	stopUpload := func() {
		select {
		case <-uploadDone:
		default:
			if raw, ok := r.Context().Value(frontendConnectionKey{}).(net.Conn); ok {
				r.Close = true
				w.Header().Set("Connection", "close")
				_ = raw.SetReadDeadline(time.Now())
			}
		}
	}
	defer func() {
		stopUpload()
		if r.Body != nil {
			_ = r.Body.Close()
		}
	}()
	var response brokerResponse
	if err = readBrokerJSON(conn, &response); err != nil || response.Status < 200 || response.Status > 599 {
		stopUpload()
		writeError(w, 502, "Invalid response from the management broker.")
		return
	}
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	// Closing a server Body alone can block behind an in-progress Read.
	stopUpload()
	w.WriteHeader(response.Status)
	if flush, ok := w.(http.Flusher); ok {
		flush.Flush()
	}
	if _, err = io.Copy(brokerFlushingWriter{w}, &brokerBodyReader{source: conn, remaining: brokerStreamLimit}); err != nil {
		// Do not terminate a truncated archive with a successful HTTP chunk.
		panic(http.ErrAbortHandler)
	}
}

type brokerFlushingWriter struct{ http.ResponseWriter }

func (w brokerFlushingWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if flush, ok := w.ResponseWriter.(http.Flusher); ok {
		flush.Flush()
	}
	return n, err
}

func brokerAddress(bind string) error {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("the unprivileged web service must bind to a loopback IP")
	}
	return nil
}
