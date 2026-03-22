package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
)

//go:embed login.html
var loginFS embed.FS

var loginTmpl = template.Must(
	template.New("login.html").Funcs(template.FuncMap{
		"join": strings.Join,
	}).ParseFS(loginFS, "login.html"),
)

type loginPageData struct {
	Users  []UserProfile
	Prefix string
	Rd     string
}

func handleSignInPage(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := loginPageData{
			Users:  cfg.Users,
			Prefix: cfg.ProxyPrefix,
			Rd:     r.URL.Query().Get("rd"),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := loginTmpl.Execute(w, data); err != nil {
			log.Printf("template error: %v", err)
		}
	}
}

func handleSignIn(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		email := r.FormValue("email")
		if email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}

		user := r.FormValue("user")
		if user == "" {
			user = email
		}
		preferredUsername := r.FormValue("preferred_username")
		if preferredUsername == "" {
			preferredUsername = user
		}

		var groups []string
		if raw := r.FormValue("groups"); raw != "" {
			for _, g := range strings.Split(raw, ",") {
				if g = strings.TrimSpace(g); g != "" {
					groups = append(groups, g)
				}
			}
		}

		session := &Session{
			User:              user,
			Email:             email,
			Groups:            groups,
			PreferredUsername:  preferredUsername,
		}

		setSessionCookie(w, session, cfg)

		rd := r.FormValue("rd")
		if !isValidRedirect(rd) {
			rd = "/"
		}
		http.Redirect(w, r, rd, http.StatusFound)
	}
}

func isValidRedirect(rd string) bool {
	if rd == "" {
		return false
	}
	if !strings.HasPrefix(rd, "/") {
		return false
	}
	if strings.HasPrefix(rd, "//") {
		return false
	}
	return true
}

func handleAuth(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := getSession(r, cfg)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Auth-Request-User", session.User)
		w.Header().Set("X-Auth-Request-Email", session.Email)
		w.Header().Set("X-Auth-Request-Groups", strings.Join(session.Groups, ","))
		w.Header().Set("X-Auth-Request-Preferred-Username", session.PreferredUsername)
		w.WriteHeader(http.StatusAccepted)
	}
}

func handleUserInfo(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := getSession(r, cfg)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"email":             session.Email,
			"user":              session.User,
			"groups":            session.Groups,
			"preferredUsername":  session.PreferredUsername,
		})
	}
}

func handleSignOut(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearSessionCookie(w, cfg)
		rd := r.URL.Query().Get("rd")
		if !isValidRedirect(rd) {
			rd = cfg.ProxyPrefix + "/sign_in"
		}
		http.Redirect(w, r, rd, http.StatusFound)
	}
}

func handleStart(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := cfg.ProxyPrefix + "/sign_in"
		if rd := r.URL.Query().Get("rd"); rd != "" {
			target += "?rd=" + url.QueryEscape(rd)
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
