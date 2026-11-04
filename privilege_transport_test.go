package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBrokerFramesBoundMetadataAndStreamArchives(t *testing.T) {
	var wire bytes.Buffer
	payload := bytes.Repeat([]byte("archive-data"), 100000)
	writer := brokerBodyWriter{target: &wire, remaining: int64(len(payload))}
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writeBrokerFrame(&wire, nil); err != nil {
		t.Fatal(err)
	}
	reader := &brokerBodyReader{source: &wire, remaining: int64(len(payload))}
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("stream did not round trip", err)
	}
	var invalid bytes.Buffer
	_ = binary.Write(&invalid, binary.BigEndian, uint32(brokerFrameLimit+1))
	if _, err = readBrokerFrame(&invalid); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err = writeBrokerFrame(io.Discard, make([]byte, brokerFrameLimit+1)); err == nil {
		t.Fatal("oversized metadata emitted")
	}
	if _, err = io.ReadAll(&brokerBodyReader{source: bytes.NewReader(nil), remaining: 10}); err != io.ErrUnexpectedEOF {
		t.Fatal("truncated stream was treated as successful EOF", err)
	}
	var bounded bytes.Buffer
	writer = brokerBodyWriter{target: &bounded, remaining: 3}
	if _, err = writer.Write([]byte("four")); err == nil || bounded.Len() != 0 {
		t.Fatal("body cap was bypassed")
	}
}

func TestBrokerOperationsAreRegisteredAndNeverRootShells(t *testing.T) {
	mux := (&App{}).routeMux()
	for _, path := range []string{"/api/version", "/api/state", "/api/sites/fixture-id", "/api/files/content", "/internal/phpmyadmin-ticket"} {
		op, suffix, err := brokerOperation(mux, path)
		if err != nil || op+suffix != path {
			t.Fatalf("registered operation %s rejected: %v", path, err)
		}
	}
	for _, path := range []string{"/", "/etc/shadow", "/api/missing-operation", "/api/terminal", "/api/terminal/session", "/api/sites/../terminal"} {
		if _, _, err := brokerOperation(mux, path); err == nil {
			t.Fatalf("unsafe/unregistered operation %s accepted", path)
		}
	}
	for _, bind := range []string{"0.0.0.0:9090", ":9090", "example.com:9090"} {
		if brokerAddress(bind) == nil {
			t.Fatal("non-loopback bind accepted", bind)
		}
	}
	for _, bind := range []string{"127.0.0.1:9090", "[::1]:9090"} {
		if err := brokerAddress(bind); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrokerResponsePreservesCookiesAndStreams(t *testing.T) {
	var wire bytes.Buffer
	w := &brokerResponseWriter{conn: &wire, header: make(http.Header), body: brokerBodyWriter{target: &wire, remaining: 100}}
	w.Header().Add("Set-Cookie", "one=value; HttpOnly")
	w.Header().Add("Set-Cookie", "two=value; Secure")
	w.WriteHeader(201)
	_, _ = io.WriteString(w, "first")
	w.Flush()
	_, _ = io.WriteString(w, "second")
	_ = writeBrokerFrame(&wire, nil)
	var response brokerResponse
	if err := readBrokerJSON(&wire, &response); err != nil {
		t.Fatal(err)
	}
	if response.Status != 201 || len(response.Header.Values("Set-Cookie")) != 2 {
		t.Fatal("status or cookies lost")
	}
	data, err := io.ReadAll(&brokerBodyReader{source: &wire, remaining: 100})
	if err != nil || string(data) != "firstsecond" {
		t.Fatal("response stream lost", err)
	}
	if strings.Contains(string(data), "Set-Cookie") {
		t.Fatal("metadata leaked into body")
	}
}
