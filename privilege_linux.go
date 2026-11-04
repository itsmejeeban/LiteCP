//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func unixPeerUID(conn net.Conn) (uint32, error) {
	local, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("Unix connection required")
	}
	raw, err := local.SyscallConn()
	if err != nil {
		return 0, err
	}
	var uid uint32
	var peerErr error
	err = raw.Control(func(fd uintptr) {
		var cred *unix.Ucred
		cred, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if peerErr == nil {
			uid = cred.Uid
		}
	})
	if err != nil {
		return 0, err
	}
	return uid, peerErr
}

func markBrokerDescriptorCloseOnExec(fd int) error {
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		return err
	}
	unix.CloseOnExec(fd)
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		return err
	}
	if flags&unix.FD_CLOEXEC == 0 {
		return errors.New("broker activation descriptor could leak to child processes")
	}
	return nil
}

func prepareBrokerActivation() error {
	if os.Geteuid() != 0 {
		return errors.New("the management broker must run as root")
	}
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		return errors.New("start the broker through litecp-broker.socket")
	}
	return markBrokerDescriptorCloseOnExec(3)
}

func runPrivilegeBroker(app *App) error {
	if os.Geteuid() != 0 {
		return errors.New("the management broker must run as root")
	}
	account, err := user.Lookup("litecp-web")
	if err != nil {
		return err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 {
		return errors.New("invalid web service UID")
	}
	if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
		return errors.New("start the broker through litecp-broker.socket; exactly one inherited socket is required")
	}
	file := os.NewFile(3, "litecp-broker.socket")
	listener, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	defer listener.Close()
	if listener.Addr().Network() != "unix" || listener.Addr().String() != brokerSocket {
		return errors.New("unexpected broker listener")
	}
	mux := app.routeMux()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	go func() { <-stop; _ = listener.Close() }()
	// Reject excess connections instead of spawning unlimited privileged workers.
	workers := make(chan struct{}, 64)
	var active sync.WaitGroup
	log.Print("LiteCP management broker accepting local credential-checked operations")
	for {
		conn, e := listener.Accept()
		if e != nil {
			if errors.Is(e, net.ErrClosed) {
				done := make(chan struct{})
				go func() { active.Wait(); close(done) }()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
				}
				return nil
			}
			return e
		}
		select {
		case workers <- struct{}{}:
			active.Add(1)
			go func() {
				defer func() {
					active.Done()
					<-workers
					if value := recover(); value != nil {
						log.Print("broker operation panicked")
					}
				}()
				serveBrokerRequest(conn, app, mux, uint32(uid))
			}()
		default:
			_ = conn.Close()
		}
	}
}

type staticPolicy struct {
	mu      sync.Mutex
	expires time.Time
	csp     string
}
type policyRecorder struct {
	header http.Header
	status int
}

func (p *policyRecorder) Header() http.Header            { return p.header }
func (p *policyRecorder) WriteHeader(status int)         { p.status = status }
func (p *policyRecorder) Write(data []byte) (int, error) { return len(data), nil }
func (p *staticPolicy) apply(w http.ResponseWriter, r *http.Request, mux *http.ServeMux) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Now().After(p.expires) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		probe := r.Clone(ctx)
		probe.Method = http.MethodGet
		probe.Body = http.NoBody
		probe.ContentLength = 0
		probe.URL.Path = "/api/version"
		probe.URL.RawQuery = ""
		probe.Header = make(http.Header)
		capture := &policyRecorder{header: make(http.Header)}
		forwardBrokerRequest(capture, probe, mux, brokerSocket)
		if capture.status == 200 {
			p.csp = capture.header.Get("Content-Security-Policy")
			p.expires = time.Now().Add(30 * time.Second)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	csp := p.csp
	if csp == "" {
		csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
	}
	w.Header().Set("Content-Security-Policy", csp)
}

func runUnprivilegedWeb() error {
	if os.Geteuid() == 0 || os.Getuid() == 0 {
		return errors.New("refusing to run the network-facing web service as root")
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("cannot prevent privilege escalation: %w", err)
	}
	account, err := user.Lookup("litecp-web")
	if err != nil || account.Uid != strconv.Itoa(os.Geteuid()) {
		return errors.New("run the web service as the dedicated litecp-web account")
	}
	if os.Getuid() != os.Geteuid() || account.Gid != strconv.Itoa(os.Getegid()) || os.Getgid() != os.Getegid() {
		return errors.New("web service real/effective identity must match its dedicated account")
	}
	groups, err := os.Getgroups()
	if err != nil {
		return err
	}
	for _, group := range groups {
		if group != os.Getegid() {
			return errors.New("web service must not have supplementary group access")
		}
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(status), "\n") {
		for _, field := range []string{"CapEff:", "CapPrm:", "CapInh:", "CapAmb:"} {
			if strings.HasPrefix(line, field) && strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, field)), "0") != "" {
				return errors.New("web service must not retain Linux capabilities")
			}
		}
	}
	bind := envOr("LITECP_BIND", "127.0.0.1:9090")
	if err = brokerAddress(bind); err != nil {
		return err
	}
	webFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		return err
	}
	static := cachedImageAssets(http.FileServer(http.FS(webFS)), webFS)
	mux := (&App{}).routeMux() // Registration only: privileged handlers are never called here.
	policy := &staticPolicy{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/") {
			forwardBrokerRequest(w, r, mux, brokerSocket)
			return
		}
		policy.apply(w, r, mux)
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		static.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: bind, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 45 * time.Minute, WriteTimeout: 45 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	server.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
		return context.WithValue(ctx, frontendConnectionKey{}, conn)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("LiteCP %s unprivileged web service UID=%d listening on %s", version, os.Geteuid(), bind)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("web listener: %w", err)
}
