package fileserver

import (
	"crypto/subtle"
	"net/http"
)

type BasicAuth struct {
	Username string
	Password string
}

func BasicAuthMiddleware(auth BasicAuth) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Allow OPTIONS to help WebDAV clients probe capabilities.
			if r.Method == "OPTIONS" {
				next.ServeHTTP(w, r)
				return
			}
			u, p, ok := r.BasicAuth()
			if ok {
				uok := subtle.ConstantTimeCompare([]byte(u), []byte(auth.Username)) == 1
				pok := subtle.ConstantTimeCompare([]byte(p), []byte(auth.Password)) == 1
				if uok && pok {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="xtpro File Share"`)
			w.Header().Set("Dav", "1, 2")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}
}
