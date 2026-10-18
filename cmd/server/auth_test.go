package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePasswordHash(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	hexHash := hex.EncodeToString(sum[:])
	cases := []string{
		hexHash,
		"{SHA256}" + hexHash,
		"{sha256}" + hexHash,
		"{SHA256}" + base64.StdEncoding.EncodeToString(sum[:]),
		base64.RawStdEncoding.EncodeToString(sum[:]),
	}
	for _, value := range cases {
		verify, err := parsePasswordHash(value)
		if err != nil {
			t.Errorf("parsePasswordHash(%q): %v", value, err)
			continue
		}
		if !verify("secret") {
			t.Errorf("hash %q rejected the correct password", value)
		}
		if verify("wrong") {
			t.Errorf("hash %q accepted the wrong password", value)
		}
	}

	for _, value := range []string{"", "{SHA256}", "{SHA256}not-a-hash", "abc"} {
		if _, err := parsePasswordHash(value); err == nil {
			t.Errorf("parsePasswordHash(%q) should fail", value)
		}
	}
}

func TestBasicAuthHandler(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	hash := "{SHA256}" + hex.EncodeToString(sum[:])
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler, err := basicAuthHandler(next, "admin", hash)
	if err != nil {
		t.Fatal(err)
	}

	// No credentials.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no auth = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("WWW-Authenticate header is missing")
	}

	// Wrong password.
	req := httptest.NewRequest("GET", "/health", nil)
	req.SetBasicAuth("admin", "wrong")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password = %d, want 401", rec.Code)
	}

	// Wrong username.
	req = httptest.NewRequest("GET", "/health", nil)
	req.SetBasicAuth("root", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong username = %d, want 401", rec.Code)
	}

	// Correct credentials.
	req = httptest.NewRequest("GET", "/health", nil)
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("correct auth = %d %q, want 200 ok", rec.Code, rec.Body.String())
	}
}

func TestBasicAuthDisabledWithoutHash(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler, err := basicAuthHandler(next, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Code != http.StatusOK || !called {
		t.Errorf("empty hash should pass through, got %d called=%v", rec.Code, called)
	}
}
