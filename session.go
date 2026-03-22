package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Session holds the authenticated user data stored in the cookie.
type Session struct {
	User              string `json:"user"`
	Email             string `json:"email"`
	Groups            []string `json:"groups"`
	PreferredUsername  string `json:"preferred_username"`
	ExpiresAt         int64  `json:"exp"`
}

func sign(data, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return mac.Sum(nil)
}

func encodeSession(s *Session, secret []byte) (string, error) {
	payload, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	b64Payload := base64.RawURLEncoding.EncodeToString(payload)
	sig := sign(payload, secret)
	b64Sig := base64.RawURLEncoding.EncodeToString(sig)
	return b64Payload + "." + b64Sig, nil
}

func decodeSession(cookie string, secret []byte) (*Session, error) {
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid cookie format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	expected := sign(payload, secret)
	if !hmac.Equal(sigBytes, expected) {
		return nil, errors.New("invalid signature")
	}

	var s Session
	if err := json.Unmarshal(payload, &s); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	if s.ExpiresAt > 0 && time.Now().Unix() > s.ExpiresAt {
		return nil, errors.New("session expired")
	}

	return &s, nil
}

func setSessionCookie(w http.ResponseWriter, s *Session, cfg *Config) {
	s.ExpiresAt = time.Now().Add(cfg.CookieExpire).Unix()
	value, err := encodeSession(s, cfg.CookieSecret)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.CookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cfg.CookieExpire.Seconds()),
	})
}

func getSession(r *http.Request, cfg *Config) (*Session, error) {
	c, err := r.Cookie(cfg.CookieName)
	if err != nil {
		return nil, err
	}
	return decodeSession(c.Value, cfg.CookieSecret)
}

func clearSessionCookie(w http.ResponseWriter, cfg *Config) {
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}
