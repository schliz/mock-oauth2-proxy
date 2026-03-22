package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func newAuthProxy(cfg *Config) http.Handler {
	target, err := url.Parse(cfg.Upstream)
	if err != nil {
		log.Fatalf("invalid upstream URL %q: %v", cfg.Upstream, err)
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.SetXForwarded()

			session, err := getSession(r.In, cfg)
			if err != nil {
				return
			}

			r.Out.Header.Set("X-Auth-Request-User", session.User)
			r.Out.Header.Set("X-Auth-Request-Email", session.Email)
			r.Out.Header.Set("X-Auth-Request-Groups", strings.Join(session.Groups, ","))
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
