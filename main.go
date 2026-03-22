package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// UserProfile defines a preconfigured user shown on the login page.
type UserProfile struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	User              string `json:"user"`
	Groups            string `json:"groups"`
	PreferredUsername  string `json:"preferred_username"`
}

// Config holds all runtime configuration parsed from environment variables.
type Config struct {
	ListenAddr   string
	Upstream     string
	CookieName   string
	CookieSecret []byte
	CookieExpire time.Duration
	ProxyPrefix  string
	Users        []UserProfile
}

func loadConfig() (*Config, error) {
	cfg := &Config{
		ListenAddr:   envOr("MOCK_LISTEN_ADDR", ":4180"),
		Upstream:     os.Getenv("MOCK_UPSTREAM"),
		CookieName:   envOr("MOCK_COOKIE_NAME", "_oauth2_proxy"),
		ProxyPrefix:  envOr("MOCK_PROXY_PREFIX", "/oauth2"),
	}

	if cfg.Upstream == "" {
		return nil, fmt.Errorf("MOCK_UPSTREAM is required")
	}

	// Cookie secret
	if secret := os.Getenv("MOCK_COOKIE_SECRET"); secret != "" {
		cfg.CookieSecret = []byte(secret)
	} else {
		cfg.CookieSecret = make([]byte, 32)
		if _, err := rand.Read(cfg.CookieSecret); err != nil {
			return nil, fmt.Errorf("generate cookie secret: %w", err)
		}
		log.Println("Generated random cookie secret (sessions won't survive restarts)")
	}

	// Cookie expiry
	expireStr := envOr("MOCK_COOKIE_EXPIRE", "168h")
	expire, err := time.ParseDuration(expireStr)
	if err != nil {
		return nil, fmt.Errorf("parse MOCK_COOKIE_EXPIRE %q: %w", expireStr, err)
	}
	cfg.CookieExpire = expire

	// Users
	users, err := loadUsers()
	if err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	cfg.Users = users

	return cfg, nil
}

func loadUsers() ([]UserProfile, error) {
	var data []byte

	if raw := os.Getenv("MOCK_USERS"); raw != "" {
		data = []byte(raw)
	} else if path := os.Getenv("MOCK_USERS_FILE"); path != "" {
		var err error
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read users file %q: %w", path, err)
		}
	}

	if data == nil {
		return []UserProfile{{
			ID:    "dev",
			Email: "dev@localhost",
			User:  "dev",
		}}, nil
	}

	var users []UserProfile
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("parse users JSON: %w", err)
	}

	for i := range users {
		if users[i].ID == "" {
			return nil, fmt.Errorf("user at index %d: id is required", i)
		}
		if users[i].Email == "" {
			return nil, fmt.Errorf("user %q: email is required", users[i].ID)
		}
		if users[i].User == "" {
			users[i].User = users[i].Email
		}
		if users[i].PreferredUsername == "" {
			users[i].PreferredUsername = users[i].User
		}
	}

	return users, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	mux := http.NewServeMux()

	prefix := cfg.ProxyPrefix
	mux.HandleFunc("GET "+prefix+"/sign_in", handleSignInPage(cfg))
	mux.HandleFunc("POST "+prefix+"/sign_in", handleSignIn(cfg))
	mux.HandleFunc("GET "+prefix+"/start", handleStart(cfg))
	mux.HandleFunc("GET "+prefix+"/callback", handleCallback)
	mux.HandleFunc("GET "+prefix+"/sign_out", handleSignOut(cfg))
	mux.HandleFunc("GET "+prefix+"/auth", handleAuth(cfg))
	mux.HandleFunc("GET "+prefix+"/userinfo", handleUserInfo(cfg))
	mux.HandleFunc("GET /ping", handlePing)
	mux.Handle("/", newAuthProxy(cfg))

	srv := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}

	go func() {
		log.Printf("Listening on %s (upstream: %s)", cfg.ListenAddr, cfg.Upstream)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
}
