//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBrokerUnprivilegedNetworkChild(t *testing.T) {
	if os.Getenv("LITECP_QA_UNPRIVILEGED_CHILD") != "1" {
		t.Skip("subprocess fixture")
	}
	if os.Geteuid() == 0 {
		t.Fatal("frontend fixture retained root")
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "CapEff:") && strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "CapEff:")), "0") != "" {
			t.Fatal("frontend retained capabilities")
		}
	}
	if file, err := os.Open(os.Getenv("LITECP_QA_PRIVATE_FILE")); err == nil {
		file.Close()
		t.Fatal("frontend could read root private data")
	}
	mux := (&App{}).routeMux()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardBrokerRequest(w, r, mux, os.Getenv("LITECP_QA_SOCKET"))
	}), ReadHeaderTimeout: 5 * time.Second}
	server.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
		return context.WithValue(ctx, frontendConnectionKey{}, conn)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"address": listener.Addr().String(), "uid": os.Geteuid()}); err != nil {
		t.Fatal(err)
	}
	_ = server.Serve(listener)
}

func TestBrokerRealUnprivilegedFrontendProcess(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to launch the unprivileged process fixture")
	}
	dir := t.TempDir()
	if err := os.Chmod(filepath.Dir(dir), 0711); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(dir, "root-private")
	if err := os.WriteFile(private, []byte("private-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = os.Chown(path, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}, sessions: map[string]session{}}
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			go serveBrokerRequest(conn, app, app.routeMux(), 65534)
		}
	}()
	executable, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer executable.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/proc/self/fd/3", "-test.run=^TestBrokerUnprivilegedNetworkChild$")
	command.ExtraFiles = []*os.File{executable}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
	command.Env = []string{"LITECP_QA_UNPRIVILEGED_CHILD=1", "LITECP_QA_SOCKET=" + path, "LITECP_QA_PRIVATE_FILE=" + private, "PATH=/usr/bin:/bin"}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	var child struct {
		Address string `json:"address"`
		UID     int    `json:"uid"`
	}
	if err = json.NewDecoder(output).Decode(&child); err != nil {
		t.Fatalf("unprivileged fixture failed: %v %s", err, stderr.String())
	}
	if child.UID != 65534 {
		t.Fatal("unexpected frontend UID", child.UID)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, fixture := range []struct {
		path   string
		status int
	}{{"/api/version", 200}, {"/api/state", 401}, {"/api/terminal/session", 403}} {
		response, e := client.Get("http://" + child.Address + fixture.path)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != fixture.status {
			t.Fatal(fmt.Sprintf("%s: %d", fixture.path, response.StatusCode))
		}
	}
}

func testBrokerConnection(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	client, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	return client, server
}

func TestBrokerRejectsUnauthenticatedSlowUploadWithoutWaitingForBody(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root-owned broker peer")
	}
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}, sessions: map[string]session{}}
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, e := listener.Accept()
		if e == nil {
			serveBrokerRequest(conn, app, app.routeMux(), 0)
		}
	}()
	frontend := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwardBrokerRequest(w, r, app.routeMux(), path) }))
	frontend.Config.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
		return context.WithValue(ctx, frontendConnectionKey{}, conn)
	}
	frontend.Start()
	defer frontend.Close()
	client, err := net.Dial("tcp", frontend.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = io.WriteString(client, "POST /api/sites HTTP/1.1\r\nHost: panel.test\r\nOrigin: http://panel.test\r\nContent-Length: 1000000\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal("early authentication rejection blocked on request body", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unexpected rejection", response.StatusCode)
	}
}

func TestBrokerFrontendStreamsAuthenticatedRequestAndCookies(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("frontend must verify a root-owned broker peer")
	}
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	app := &App{sessions: map[string]session{"stream-token": {Expires: time.Now().Add(time.Hour)}}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", app.withAuth(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(brokerPeerUIDKey{}).(uint32); !ok {
			t.Error("missing verified identity")
		}
		w.Header().Add("Set-Cookie", "fixture=one; HttpOnly")
		w.Header().Add("Set-Cookie", "fixture=two; Secure")
		w.Header().Set("X-Fixture", r.URL.Query().Get("fixture"))
		_, _ = io.Copy(w, r.Body)
	}))
	go func() {
		conn, e := listener.Accept()
		if e == nil {
			serveBrokerRequest(conn, app, mux, 0)
		}
	}()
	data := bytes.Repeat([]byte("stream-fixture-"), 100000)
	r := httptest.NewRequest("POST", "http://panel.test/api/state?fixture=passed", bytes.NewReader(data))
	r.Header.Set("Cookie", "litecp_session=stream-token")
	r.Header.Set("Origin", "http://panel.test")
	w := httptest.NewRecorder()
	forwardBrokerRequest(w, r, mux, path)
	if w.Code != 200 || !bytes.Equal(data, w.Body.Bytes()) || len(w.Header().Values("Set-Cookie")) != 2 || w.Header().Get("X-Fixture") != "passed" {
		t.Fatal("authenticated stream/metadata did not round trip", w.Code, w.Body.Len())
	}
	if !w.Flushed {
		t.Fatal("streaming response was buffered")
	}
}

func TestBrokerPeerCredentialsAndUnauthenticatedAPI(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}, sessions: map[string]session{}}
	client, server := testBrokerConnection(t)
	if uid, err := unixPeerUID(client); err != nil || uid != uint32(os.Geteuid()) {
		t.Fatal("peer identity mismatch", uid, err)
	}
	go serveBrokerRequest(server, app, app.routeMux(), uint32(os.Geteuid()))
	if err = writeBrokerJSON(client, brokerRequest{Operation: "/api/state", Method: "GET", Host: "panel.test", Header: make(http.Header)}); err != nil {
		t.Fatal(err)
	}
	_ = writeBrokerFrame(client, nil)
	var response brokerResponse
	if err = readBrokerJSON(client, &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusUnauthorized {
		t.Fatal("broker bypassed authentication", response.Status)
	}
	_, _ = io.Copy(io.Discard, &brokerBodyReader{source: client, remaining: brokerStreamLimit})
	other, otherServer := testBrokerConnection(t)
	go serveBrokerRequest(otherServer, app, app.routeMux(), uint32(os.Geteuid())+1)
	_, err = other.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("wrong UID was admitted")
	}
}

func TestNetworkWebRefusesRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to verify fail-closed startup")
	}
	if err := runUnprivilegedWeb(); err == nil {
		t.Fatal("root web startup accepted")
	}
	if !runtimeRootHTTPForbidden([]string{"litecp"}, false) {
		t.Fatal("legacy root HTTP startup accepted")
	}
	if runtimeRootHTTPForbidden([]string{"litecp", "broker"}, false) {
		t.Fatal("local broker startup incorrectly rejected")
	}
}

func TestBrokerActivationDescriptorCannotLeakToWebsiteChildren(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "descriptor-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fd := file.Fd()
	flags, err := unix.FcntlInt(fd, unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = unix.FcntlInt(fd, unix.F_SETFD, flags&^unix.FD_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	if err = markBrokerDescriptorCloseOnExec(int(fd)); err != nil {
		t.Fatal(err)
	}
	flags, err = unix.FcntlInt(fd, unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("descriptor remains inheritable", err)
	}
}

func TestBrokerAuthenticatedOperationsAndIdentity(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &App{store: store, system: &System{demo: true}, sessions: map[string]session{"test-token": {Expires: time.Now().Add(time.Hour)}}}
	client, server := testBrokerConnection(t)
	go serveBrokerRequest(server, app, app.routeMux(), uint32(os.Geteuid()))
	header := make(http.Header)
	header.Set("Cookie", "litecp_session=test-token")
	_ = writeBrokerJSON(client, brokerRequest{Operation: "/api/security/posture", Method: "GET", Host: "panel.test", Header: header})
	_ = writeBrokerFrame(client, nil)
	var response brokerResponse
	if err = readBrokerJSON(client, &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != 200 {
		t.Fatal("authenticated operation failed", response.Status)
	}
	data, err := io.ReadAll(&brokerBodyReader{source: client, remaining: brokerStreamLimit})
	if err != nil {
		t.Fatal(err)
	}
	var posture securityPosture
	if json.Unmarshal(data, &posture) != nil || len(posture.Checks) != 1 {
		t.Fatal("invalid posture response")
	}
	if bytes.Contains(data, []byte("test-token")) {
		t.Fatal("session token leaked")
	}
}
