# mock-oauth2-proxy — Implementierungsplan

> Drop-in-Replacement für oauth2-proxy in Entwicklungsumgebungen.
> Click-to-Login statt OIDC-Flow. Vordefinierte User-Profile. Ein Binary, zero Dependencies.

**Repository:** `github.com/schliz/mock-oauth2-proxy`
**Image:** `ghcr.io/schliz/mock-oauth2-proxy`

---

## 1. Architektur-Überblick

```
Browser ──► mock-oauth2-proxy (:4180) ──► Upstream App (:8080)
                │
                ├── /oauth2/sign_in    → Login-Seite (User-Buttons + Custom-Formular)
                ├── /oauth2/start      → Redirect zu /oauth2/sign_in
                ├── /oauth2/callback   → Redirect zu / (No-Op, Kompatibilität)
                ├── /oauth2/sign_out   → Cookie löschen, optionaler rd-Redirect
                ├── /oauth2/auth       → 202 + Headers oder 401 (nginx auth_request)
                ├── /oauth2/userinfo   → JSON mit Session-Daten
                ├── /ping              → 200 OK
                └── /*                 → Reverse Proxy mit injizierten Headern
```

Der Proxy verhält sich identisch zum echten oauth2-proxy aus Sicht der
Downstream-App: gleiche Endpunkte, gleiche Header, gleiches Cookie-Verhalten.

---

## 2. Multi-User-Konfiguration

### Konzept

Statt eines einzelnen Default-Users wird eine **Map von User-Profilen** definiert.
Die Login-Seite zeigt für jedes Profil einen Button an — ein Klick loggt direkt ein.
Zusätzlich gibt es ein aufklappbares "Custom Login"-Formular für ad-hoc-User.

### Konfiguration via `MOCK_USERS` (JSON)

```bash
MOCK_USERS='[
  {
    "id": "admin",
    "email": "admin@example.com",
    "user": "admin",
    "groups": "admin,k4-bar",
    "preferred_username": "admin"
  },
  {
    "id": "member",
    "email": "member@hadiko.de",
    "user": "member",
    "groups": "k4-bar",
    "preferred_username": "member"
  },
  {
    "id": "guest",
    "email": "guest@example.com",
    "user": "guest",
    "groups": "",
    "preferred_username": "gast"
  }
]'
```

Alternativ via Datei: `MOCK_USERS_FILE=/etc/mock-oauth2-proxy/users.json`

**Regeln:**

- `id` ist Required und muss eindeutig sein (wird als Button-Label und interner Key benutzt)
- `email` ist Required
- `user` ist Optional, Default = `email`
- `groups` ist Optional, Default = `""`
- `preferred_username` ist Optional, Default = `user`
- Wenn weder `MOCK_USERS` noch `MOCK_USERS_FILE` gesetzt ist, wird ein
  einzelner Default-User `dev / dev@localhost / ""` angelegt

### Login-Seite

```
┌──────────────────────────────────────┐
│        Mock OAuth2 Proxy             │
│        Development Login             │
│                                      │
│  ┌──────────────────────────────┐    │
│  │  🔑  admin                   │    │
│  │  admin@example.com           │    │
│  │  Groups: admin, k4-bar       │    │
│  └──────────────────────────────┘    │
│                                      │
│  ┌──────────────────────────────┐    │
│  │  🔑  member                  │    │
│  │  member@hadiko.de            │    │
│  │  Groups: k4-bar              │    │
│  └──────────────────────────────┘    │
│                                      │
│  ┌──────────────────────────────┐    │
│  │  🔑  guest                   │    │
│  │  guest@example.com           │    │
│  │  Groups: —                   │    │
│  └──────────────────────────────┘    │
│                                      │
│  ▸ Custom Login...                   │
│  ┌──────────────────────────────┐    │
│  │ Email:    [________________] │    │
│  │ User:     [________________] │    │
│  │ Groups:   [________________] │    │
│  │ Pref.User:[________________] │    │
│  │         [ Login ]            │    │
│  └──────────────────────────────┘    │
└──────────────────────────────────────┘
```

Jeder User-Button ist ein `<form>` mit hidden fields, die per POST
an `/oauth2/sign_in` gehen. Das Custom-Formular ist ein `<details>`-Element
(nativ auf/zuklappbar, kein JS nötig).

---

## 3. Dateien und Verantwortlichkeiten

Das gesamte Projekt liegt in einem einzigen Go-Package (`package main`).
Keine `internal/`, kein `cmd/` — die Komplexität rechtfertigt es nicht.

### `main.go` — Entrypoint, Config, Routing

Verantwortung:
- Config aus Environment parsen (`MOCK_*` Variablen)
- Users laden (JSON-String oder Datei)
- Mux aufbauen (alle `/oauth2/*` Routen + Catch-all Reverse Proxy)
- Graceful Shutdown

Config-Struct:

```go
type Config struct {
    ListenAddr  string        // MOCK_LISTEN_ADDR, default ":4180"
    Upstream    string        // MOCK_UPSTREAM, required
    CookieName  string       // MOCK_COOKIE_NAME, default "_oauth2_proxy"
    CookieSecret []byte      // MOCK_COOKIE_SECRET, default auto-generated
    CookieExpire time.Duration // MOCK_COOKIE_EXPIRE, default 168h
    ProxyPrefix string        // MOCK_PROXY_PREFIX, default "/oauth2"
    Users       []UserProfile // MOCK_USERS (JSON) oder MOCK_USERS_FILE
}

type UserProfile struct {
    ID                string `json:"id"`
    Email             string `json:"email"`
    User              string `json:"user"`
    Groups            string `json:"groups"`
    PreferredUsername  string `json:"preferred_username"`
}
```

Routing:

```go
prefix := cfg.ProxyPrefix // default "/oauth2"
mux.HandleFunc("GET "+prefix+"/sign_in", handleSignInPage)
mux.HandleFunc("POST "+prefix+"/sign_in", handleSignIn)
mux.HandleFunc("GET "+prefix+"/start", handleStart)
mux.HandleFunc("GET "+prefix+"/callback", handleCallback)
mux.HandleFunc("GET "+prefix+"/sign_out", handleSignOut)
mux.HandleFunc("GET "+prefix+"/auth", handleAuth)
mux.HandleFunc("GET "+prefix+"/userinfo", handleUserInfo)
mux.HandleFunc("GET /ping", handlePing)
mux.Handle("/", authProxy) // Catch-all: Reverse Proxy
```

### `session.go` — Cookie-basierte Session

Verantwortung:
- Session-Daten in HMAC-SHA256-signiertem Cookie speichern/lesen
- Kein Encryption (Dev-Tool), aber Tamper-Protection via HMAC

Datenstruktur im Cookie:

```go
type Session struct {
    User              string `json:"user"`
    Email             string `json:"email"`
    Groups            string `json:"groups"`
    PreferredUsername  string `json:"preferred_username"`
    ExpiresAt         int64  `json:"exp"`
}
```

Cookie-Format: `base64(json) + "." + base64(hmac-sha256(json, secret))`

Funktionen:

```go
func encodeSession(s *Session, secret []byte) (string, error)
func decodeSession(cookie string, secret []byte) (*Session, error)
func setSessionCookie(w http.ResponseWriter, s *Session, cfg *Config)
func getSession(r *http.Request, cfg *Config) (*Session, error)
func clearSessionCookie(w http.ResponseWriter, cfg *Config)
```

### `handlers.go` — HTTP Handler

Verantwortung:
- Login-Seite rendern (mit User-Buttons + Custom-Formular)
- Sign-in: Session-Cookie setzen, Redirect
- Sign-out: Cookie löschen
- Auth: 202/401 + Response-Headers
- Userinfo: JSON Response
- Start/Callback: Kompatibilitäts-Redirects

Handler-Details:

**`GET /oauth2/sign_in`**
- Template rendern mit allen `cfg.Users`
- `rd` Query-Parameter in hidden field durchreichen
- Inline-CSS, kein externes Dependency

**`POST /oauth2/sign_in`**
- Form-Values lesen: `email`, `user`, `groups`, `preferred_username`
- Session-Cookie setzen
- Redirect zu `rd`-Parameter (wenn vorhanden + validiert) oder `/`
- **Redirect-Validierung:** Nur relative Pfade erlauben (beginnt mit `/`,
  kein `//`), keine externen URLs → Open-Redirect-Schutz

**`GET /oauth2/auth`**
- Session aus Cookie lesen
- Wenn gültig: 202 + folgende Response-Header:
  - `X-Auth-Request-User`
  - `X-Auth-Request-Email`
  - `X-Auth-Request-Groups`
  - `X-Auth-Request-Preferred-Username`
- Wenn ungültig/abgelaufen: 401

**`GET /oauth2/sign_out`**
- Cookie löschen (MaxAge=-1)
- Wenn `rd`-Query-Parameter vorhanden: Redirect dorthin
- Sonst: Redirect zu `/oauth2/sign_in`

**`GET /oauth2/userinfo`**
- Session lesen → JSON: `{"email": "...", "user": "...", "groups": "...", "preferredUsername": "..."}`
- 401 wenn keine Session

**`GET /oauth2/start`**
- Redirect 302 zu `/oauth2/sign_in` (mit `rd` durchreichen)

**`GET /oauth2/callback`**
- Redirect 302 zu `/` (Kompatibilität, wird nicht wirklich gebraucht)

**`GET /ping`**
- 200 "OK"

### `proxy.go` — Reverse Proxy mit Header-Injection

Verantwortung:
- `httputil.ReverseProxy` zum konfigurierten Upstream
- Bei gültiger Session: Header injizieren
- Bei ungültiger Session: Redirect zu `/oauth2/sign_in?rd=<original-path>`

```go
func newAuthProxy(cfg *Config) http.Handler {
    target, _ := url.Parse(cfg.Upstream)
    proxy := &httputil.ReverseProxy{
        Rewrite: func(r *httputil.ProxyRequest) {
            r.SetURL(target)
            r.SetXForwarded()

            session, err := getSession(r.In, cfg)
            if err != nil {
                // Wird im ServeHTTP abgefangen, nicht hier
                return
            }

            r.Out.Header.Set("X-Auth-Request-User", session.User)
            r.Out.Header.Set("X-Auth-Request-Email", session.Email)
            r.Out.Header.Set("X-Auth-Request-Groups", session.Groups)
            r.Out.Header.Set("X-Auth-Request-Preferred-Username", session.PreferredUsername)
        },
    }

    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        _, err := getSession(r, cfg)
        if err != nil {
            rd := r.URL.RequestURI()
            http.Redirect(w, r, cfg.ProxyPrefix+"/sign_in?rd="+url.QueryEscape(rd), http.StatusFound)
            return
        }
        proxy.ServeHTTP(w, r)
    })
}
```

### `login.html` — Embedded Login-Seite

Eingebettet via `//go:embed login.html` als `html/template`.

Features:
- Inline-CSS (~30 Zeilen), kein CDN, kein JS-Framework
- Schleife über `{{range .Users}}` → ein Button-Formular pro User
- `<details>` für Custom-Login-Formular (nativ auf/zuklappbar)
- Jeder User-Button: `<form method="POST">` mit hidden fields
- `rd` Parameter wird in allen Formularen als hidden field durchgereicht
- Responsiv (max-width Container, flexbox)
- Visuell klar als Dev-Tool erkennbar (z.B. gelber Banner "⚠ Development Mode")

### `Dockerfile`

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod ./
# go.sum nur wenn Dependencies existieren
COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o mock-oauth2-proxy .

FROM scratch
COPY --from=builder /build/mock-oauth2-proxy /mock-oauth2-proxy
EXPOSE 4180
ENTRYPOINT ["/mock-oauth2-proxy"]
```

### `.github/workflows/release.yml`

Trigger: Push auf `main` + Semver-Tags (`v*`)

Steps:
1. Checkout
2. Setup Go
3. `go vet ./...` + `go build`
4. Docker Buildx
5. Login zu ghcr.io
6. Build + Push multi-arch (`linux/amd64`, `linux/arm64`)
7. Tags: `latest`, `vX.Y.Z`, `vX.Y`, `vX`

---

## 4. Implementierungsreihenfolge

Die Reihenfolge ist so gewählt, dass nach jedem Schritt etwas Testbares existiert.

### Schritt 1: `go mod init` + Config + Main-Skeleton

- `go mod init github.com/schliz/mock-oauth2-proxy`
- Config-Struct + Environment-Parsing
- User-Loading (JSON-String oder Datei)
- Mux-Setup mit Placeholder-Handlern
- Graceful Shutdown
- **Testbar:** Binary startet, `/ping` antwortet 200

### Schritt 2: Session (Cookie Encode/Decode)

- `Session` Struct
- HMAC-SHA256 Sign + Verify
- `encodeSession` / `decodeSession`
- `setSessionCookie` / `getSession` / `clearSessionCookie`
- Cookie-Expiry-Prüfung
- **Testbar:** Unit-Test für Roundtrip encode→decode, Tamper-Detection

### Schritt 3: Login-Seite + Sign-In Handler

- `login.html` Template mit User-Buttons + Custom-Formular
- `GET /oauth2/sign_in` → Template rendern
- `POST /oauth2/sign_in` → Cookie setzen + Redirect
- `rd`-Parameter durchreichen + Validierung
- **Testbar:** Browser öffnen → Buttons sehen → Klicken → Cookie wird gesetzt

### Schritt 4: Auth + Userinfo + Sign-Out

- `GET /oauth2/auth` → 202 mit X-Auth-Request-* Headern oder 401
- `GET /oauth2/userinfo` → JSON Response
- `GET /oauth2/sign_out` → Cookie löschen + Redirect
- `GET /oauth2/start` → Redirect zu sign_in
- `GET /oauth2/callback` → Redirect zu /
- **Testbar:** `curl -v /oauth2/auth` mit/ohne Cookie → korrekte Responses

### Schritt 5: Reverse Proxy mit Header-Injection

- `httputil.ReverseProxy` zum Upstream
- Session prüfen → Header injizieren oder Redirect
- `X-Auth-Request-User`, `-Email`, `-Groups`, `-Preferred-Username`
- **Testbar:** Upstream-App (z.B. `httpbin` oder eigene App) empfängt korrekte Header

### Schritt 6: Dockerfile + CI

- Multi-stage Dockerfile (golang → scratch)
- GitHub Actions Workflow: Build, Push zu ghcr.io
- Multi-arch: linux/amd64, linux/arm64
- README.md mit Nutzungsanleitung + docker-compose Beispiel

---

## 5. Nutzungsbeispiel: docker-compose.dev.yml

```yaml
services:
  auth-proxy:
    image: ghcr.io/schliz/mock-oauth2-proxy:latest
    environment:
      MOCK_UPSTREAM: "http://app:8080"
      MOCK_USERS: |
        [
          {"id": "admin", "email": "admin@hadiko.de", "groups": "admin,k4-bar", "preferred_username": "admin"},
          {"id": "thekenwart", "email": "theke@hadiko.de", "groups": "k4-bar", "preferred_username": "thekenwart"},
          {"id": "bewohner", "email": "bewohner@hadiko.de", "groups": "", "preferred_username": "bewohner"}
        ]
    ports:
      - "4180:4180"

  app:
    build: .
    expose:
      - "8080"
```

Für Deckel: Drei Klicks, drei verschiedene Rollen testen.
Für Shiftings: Gleiche Idee, andere Gruppen-Config.

In Produktion: `auth-proxy` wird durch den echten oauth2-proxy + Keycloak ersetzt.
Die App merkt keinen Unterschied.

---

## 6. Nutzungsbeispiel: nginx auth_request (Caddy-Alternative)

Falls der Proxy nicht als Reverse Proxy sondern im `auth_request`-Modus
mit Caddy/Nginx verwendet wird:

```
Browser ──► Caddy ──auth_request──► mock-oauth2-proxy /oauth2/auth
                │                          │
                │                    202 + Headers / 401
                │
                └──► Upstream App (mit X-Auth-Request-* Headern von Caddy)
```

Der `/oauth2/auth` Endpoint sendet bei 202 die gleichen Response-Header
wie der echte oauth2-proxy. Caddy/Nginx kann diese per
`auth_request_set` an den Upstream weiterleiten.

---

## 7. Nicht implementiert (bewusst)

| Feature | Begründung |
|---|---|
| TLS | Caddy/Nginx davor in dev |
| Redis Sessions | Overkill für Dev, Cookie reicht |
| Token-Passing (`pass_access_token`) | Kein echter Token vorhanden |
| `set_authorization_header` | Kein echter Bearer Token |
| PKCE / Code Challenge | Kein OAuth-Flow |
| Group-Autorisierung im Proxy | Apps machen das selbst |
| `skip-auth-*` Regeln | Login ist ein Klick, nicht nötig |
| Multiple Upstreams | Ein Proxy pro App in Compose |
| Auto-Login | Immer Formular zeigen, User bewusst wählen |
