package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

//go:embed scripts/terminal-pty.py
var terminalPTY string

type interactiveTerminal struct {
	mu sync.Mutex
	inputMu sync.Mutex
	owner [32]byte
	cmd *exec.Cmd
	input io.WriteCloser
	output []byte
	base int64
	closed bool
	touched time.Time
}

var interactiveTerminals = struct { sync.Mutex; items map[string]*interactiveTerminal }{items: make(map[string]*interactiveTerminal)}

func terminalOwner(r *http.Request) [32]byte {
	for _, name := range []string{"litecp_session", "litecp_trusted"} {
		if cookie, err := r.Cookie(name); err == nil && cookie.Value != "" { return sha256.Sum256([]byte(cookie.Value)) }
	}
	return sha256.Sum256([]byte(r.Header.Get("Cookie")))
}

func (a *App) interactiveTerminal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := terminalOwner(r)
	id := r.URL.Query().Get("id")
	if r.Method == http.MethodPost && id == "" {
		if a.system.demo { writeError(w, 400, "Terminal requires the installed Linux server."); return }
		interactiveTerminals.Lock()
		defer interactiveTerminals.Unlock()
		if len(interactiveTerminals.items) >= 8 { writeError(w, 429, "Close an existing terminal before opening another."); return }
		id, err := randomToken(24)
		if err != nil { writeError(w, 500, "Cannot allocate terminal"); return }
		cmd := exec.Command("python3", "-u", "-c", terminalPTY)
		input, err := cmd.StdinPipe()
		if err != nil { writeError(w, 500, "Cannot open terminal input"); return }
		output, err := cmd.StdoutPipe()
		if err != nil { input.Close(); writeError(w, 500, "Cannot open terminal output"); return }
		if err := cmd.Start(); err != nil { input.Close(); writeError(w, 500, "Cannot start terminal; Python 3 and bash are required"); return }
		session := &interactiveTerminal{owner: owner, cmd: cmd, input: input, touched: time.Now()}
		interactiveTerminals.items[id] = session
		go func() {
			buffer := make([]byte, 16384)
			for { n, err := output.Read(buffer); if n > 0 { session.mu.Lock(); session.output = append(session.output, buffer[:n]...); if len(session.output) > 2<<20 { drop := len(session.output)-(2<<20); session.output = append([]byte(nil), session.output[drop:]...); session.base += int64(drop) }; session.mu.Unlock() }; if err != nil { break } }
			_ = cmd.Wait(); session.mu.Lock(); session.closed = true; session.mu.Unlock()
		}()
		go func() {
			ticker := time.NewTicker(time.Minute); defer ticker.Stop()
			for range ticker.C { session.mu.Lock(); expired := time.Since(session.touched) > 5*time.Minute; session.mu.Unlock(); if expired { session.inputMu.Lock(); _ = input.Close(); _ = cmd.Process.Signal(syscall.SIGTERM); session.inputMu.Unlock(); interactiveTerminals.Lock(); delete(interactiveTerminals.items,id); interactiveTerminals.Unlock(); return } }
		}()
		writeJSON(w, 201, map[string]string{"id": id}); return
	}
	interactiveTerminals.Lock(); session := interactiveTerminals.items[id]; interactiveTerminals.Unlock()
	if session == nil || session.owner != owner { writeError(w, 404, "Terminal session not found. Connect again."); return }
	session.mu.Lock(); session.touched = time.Now(); session.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"),10,64)
		session.mu.Lock(); defer session.mu.Unlock()
		reset := cursor < session.base || cursor > session.base+int64(len(session.output))
		if reset { cursor = session.base }
		writeJSON(w,200,map[string]any{"data":base64.StdEncoding.EncodeToString(session.output[cursor-session.base:]),"cursor":session.base+int64(len(session.output)),"closed":session.closed,"reset":reset})
	case http.MethodPost:
		var message struct { Data string `json:"data"`; Cols int `json:"cols"`; Rows int `json:"rows"` }
		if err := decodeJSONWithLimit(r,&message,96<<10); err != nil { writeError(w,400,"Invalid terminal input"); return }
		if _, err := base64.StdEncoding.DecodeString(message.Data); err != nil { writeError(w,400,"Invalid terminal data"); return }
		session.inputMu.Lock(); err := json.NewEncoder(session.input).Encode(message); session.inputMu.Unlock()
		if err != nil { writeError(w,409,"Terminal has closed"); return }; writeJSON(w,200,map[string]bool{"ok":true})
	case http.MethodDelete:
		session.inputMu.Lock(); _ = session.input.Close(); _ = session.cmd.Process.Signal(syscall.SIGTERM); session.inputMu.Unlock()
		interactiveTerminals.Lock(); delete(interactiveTerminals.items,id); interactiveTerminals.Unlock()
		writeJSON(w,200,map[string]bool{"closed":true})
	default: writeError(w,405,"Use GET, POST or DELETE")
	}
}
