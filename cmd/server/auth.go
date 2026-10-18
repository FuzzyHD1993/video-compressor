package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const basicAuthRealm = "Video Compressor"

// parsePasswordHash accepts:
//   - {SHA256}<hex or base64 SHA-256 digest>
//   - <hex SHA-256 digest>
//   - <base64 SHA-256 digest>
func parsePasswordHash(value string) (func(string) bool, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil, errors.New("пустой BASIC_AUTH_HASH")
	}
	if len(v) >= len("{SHA256}") && strings.EqualFold(v[:len("{SHA256}")], "{SHA256}") {
		v = strings.TrimSpace(v[len("{SHA256}"):])
	}

	digest, err := decodeSHA256(v)
	if err != nil {
		return nil, err
	}
	return func(password string) bool {
		sum := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(sum[:], digest) == 1
	}, nil
}

func decodeSHA256(value string) ([]byte, error) {
	if len(value) == sha256.Size*2 {
		if digest, err := hex.DecodeString(value); err == nil && len(digest) == sha256.Size {
			return digest, nil
		}
	}
	if digest, err := base64.StdEncoding.DecodeString(value); err == nil && len(digest) == sha256.Size {
		return digest, nil
	}
	if digest, err := base64.RawStdEncoding.DecodeString(value); err == nil && len(digest) == sha256.Size {
		return digest, nil
	}
	return nil, fmt.Errorf("BASIC_AUTH_HASH должен быть SHA-256 в hex/base64 (можно с префиксом {SHA256})")
}

// basicAuthHandler wraps next with HTTP Basic Auth. A valid hash enables auth;
// an empty hash returns next unchanged (localhost mode).
func basicAuthHandler(next http.Handler, username, hash string) (http.Handler, error) {
	if strings.TrimSpace(hash) == "" {
		return next, nil
	}
	verify, err := parsePasswordHash(hash)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(username)) == 1
		if !ok || !userOK || !verify(password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="`+basicAuthRealm+`", charset="UTF-8"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
