package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type webmailLaunch struct {
	MailboxID, Address, Session string
	Expires                     time.Time
}

var webmailTicketMu sync.Mutex
var webmailTickets = map[string]webmailLaunch{}
var webmailMasterMu sync.Mutex

func (s *System) ensureWebmailMaster(ctx context.Context) error {
	webmailMasterMu.Lock()
	defer webmailMasterMu.Unlock()
	file := filepath.Join(s.dataDir, "webmail-master.enc")
	encrypted, err := os.ReadFile(file)
	var password string
	if os.IsNotExist(err) {
		password, err = randomToken(48)
		if err != nil {
			return err
		}
		cipher, e := s.EncryptSecret(password)
		if e != nil {
			return e
		}
		if err = writeAtomic(file, []byte(cipher), 0600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		password, err = s.DecryptSecret(strings.TrimSpace(string(encrypted)))
		if err != nil {
			return err
		}
	}
	hash, err := s.HashMailboxPassword(ctx, password)
	if err != nil {
		return err
	}
	path := "/etc/litecp/mail/webmail-master-users"
	if err = writeAtomic(path, []byte("litecp-webmail-sso:"+hash+"::::::allow_nets=127.0.0.1,::1\n"), 0600); err != nil {
		return err
	}
	return chownWebTree(path, "dovecot")
}

func (a *App) webmailLaunch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, 400, "Invalid mailbox.")
		return
	}
	mailbox := a.store.MailboxByID(input.ID)
	if mailbox == nil {
		writeError(w, 404, "Mailbox not found.")
		return
	}
	if a.system.demo || !a.system.WebmailAvailable() || !a.store.Settings().EmailHostingEnabled {
		writeError(w, 503, "Webmail is unavailable.")
		return
	}
	if _, err := os.Stat("/etc/dovecot/conf.d/00-litecp-webmail-signon.conf"); err != nil {
		writeError(w, 503, "Install the webmail sign-in integration first.")
		return
	}
	if err := a.system.ensureWebmailMaster(r.Context()); err != nil {
		writeError(w, 503, "Cannot prepare secure mailbox sign-in.")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "Cannot start webmail.")
		return
	}
	sessionToken, err := a.webmailSession(w, r)
	if err != nil {
		writeError(w, 401, "Sign in again.")
		return
	}
	webmailTicketMu.Lock()
	for key, value := range webmailTickets {
		if time.Now().After(value.Expires) {
			delete(webmailTickets, key)
		}
	}
	if len(webmailTickets) >= 100 {
		webmailTicketMu.Unlock()
		writeError(w, 429, "Too many pending mailbox logins.")
		return
	}
	webmailTickets[token] = webmailLaunch{mailbox.ID, mailbox.Address, sessionToken, time.Now().Add(30 * time.Second)}
	webmailTicketMu.Unlock()
	a.store.AddActivity("Webmail login", "Administrator opened "+mailbox.Address, "info")
	writeJSON(w, 200, map[string]string{"ticket": token, "url": a.system.WebmailURL()})
}

func (a *App) consumeWebmailTicket(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || r.Host != "127.0.0.1:9090" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Forwarded-Proto") != "" {
		writeError(w, 403, "Internal access only.")
		return
	}
	var input struct {
		Ticket string `json:"ticket"`
	}
	if decodeJSONWithLimit(r, &input, 1024) != nil {
		writeError(w, 400, "Invalid sign-in ticket.")
		return
	}
	webmailTicketMu.Lock()
	launch, ok := webmailTickets[input.Ticket]
	delete(webmailTickets, input.Ticket)
	webmailTicketMu.Unlock()
	if !ok || time.Now().After(launch.Expires) || !a.validSession(launch.Session) || !a.store.Settings().EmailHostingEnabled {
		writeError(w, 403, "Mailbox login expired.")
		return
	}
	mailbox := a.store.MailboxByID(launch.MailboxID)
	if mailbox == nil || mailbox.Address != launch.Address {
		writeError(w, 403, "Mailbox unavailable.")
		return
	}
	encrypted, err := os.ReadFile(filepath.Join(a.system.dataDir, "webmail-master.enc"))
	if err != nil {
		writeError(w, 503, "Webmail sign-in unavailable.")
		return
	}
	password, err := a.system.DecryptSecret(strings.TrimSpace(string(encrypted)))
	if err != nil {
		writeError(w, 503, "Webmail sign-in unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"address": launch.Address, "password": password, "master": "litecp-webmail-sso"})
}
