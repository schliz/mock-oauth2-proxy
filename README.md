# mock-oauth2-proxy

Drop-in replacement for [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy) in development environments. Instead of a real OIDC flow, you get a login page with preconfigured user buttons.

Your downstream app sees the same endpoints, headers, and cookie behavior as with the real oauth2-proxy. Swap it in for dev, swap it out for prod.

## When to use this

- You run oauth2-proxy (or plan to) in production and need a frictionless local dev experience
- You want to test different user roles/groups without touching an identity provider
- You use nginx/Caddy `auth_request` with oauth2-proxy and need a local stand-in

## Quick start

```yaml
# docker-compose.dev.yml
services:
  auth-proxy:
    image: ghcr.io/schliz/mock-oauth2-proxy:latest
    environment:
      MOCK_UPSTREAM: "http://app:8080"
      MOCK_USERS: |
        [
          {"id": "admin", "email": "admin@example.com", "groups": ["admin", "editors"]},
          {"id": "viewer", "email": "viewer@example.com", "groups": ["viewers"]}
        ]
    ports:
      - "4180:4180"

  app:
    build: .
    expose:
      - "8080"
```

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `MOCK_UPSTREAM` | — *(required)* | URL of the upstream application |
| `MOCK_USERS` | — | JSON array of user profiles (see below) |
| `MOCK_USERS_FILE` | — | Path to a JSON file containing user profiles |
| `MOCK_LISTEN_ADDR` | `:4180` | Listen address |
| `MOCK_COOKIE_NAME` | `_oauth2_proxy` | Session cookie name |
| `MOCK_COOKIE_SECRET` | *(auto-generated)* | HMAC secret for cookie signing |
| `MOCK_COOKIE_EXPIRE` | `168h` | Session duration (resets after restart) |
| `MOCK_PROXY_PREFIX` | `/oauth2` | Path prefix for all oauth2 endpoints |

If neither `MOCK_USERS` nor `MOCK_USERS_FILE` is set, a single default user `dev` / `dev@localhost` is created.

### User profiles

```json
[
  {
    "id": "admin",
    "email": "admin@example.com",
    "user": "admin",
    "groups": ["admin", "editors"],
    "preferred_username": "admin"
  }
]
```

- `id` — required, unique, used as button label
- `email` — required
- `user` — optional, defaults to `email`
- `groups` — optional, array of strings
- `preferred_username` — optional, defaults to `user`

## Endpoints

| Endpoint | Description |
|---|---|
| `GET /oauth2/sign_in` | Login page with user buttons and custom login form |
| `POST /oauth2/sign_in` | Set session cookie and redirect |
| `GET /oauth2/auth` | Returns `202` + `X-Auth-Request-*` headers if authenticated, `401` otherwise |
| `GET /oauth2/userinfo` | JSON with session data |
| `GET /oauth2/sign_out` | Clear session, redirect |
| `GET /oauth2/start` | Redirect to sign_in (compatibility) |
| `GET /oauth2/callback` | Redirect to `/` (compatibility) |
| `GET /ping` | Health check |
| `/*` | Reverse proxy to upstream with `X-Auth-Request-*` headers injected |

The `auth` endpoint is compatible with nginx `auth_request` and Caddy `forward_auth`.

## Headers

Authenticated requests (both proxied and via `/oauth2/auth`) include:

- `X-Auth-Request-User`
- `X-Auth-Request-Email`
- `X-Auth-Request-Groups`
- `X-Auth-Request-Preferred-Username`
