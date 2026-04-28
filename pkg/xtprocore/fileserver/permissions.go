package fileserver

import (
	"net/http"
	"strings"
)

type Permissions string

const (
	PermRead      Permissions = "r"
	PermReadWrite Permissions = "rw"
	PermFull      Permissions = "rwx"
)

func ParsePermissions(s string) Permissions {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "r", "read":
		return PermRead
	case "rw", "readwrite", "read-write":
		return PermReadWrite
	case "rwx", "full":
		return PermFull
	default:
		return PermReadWrite
	}
}

func (p Permissions) HasRead() bool  { return p == PermRead || p == PermReadWrite || p == PermFull }
func (p Permissions) HasWrite() bool { return p == PermReadWrite || p == PermFull }

func PermissionMiddleware(perms Permissions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case "GET", "HEAD", "OPTIONS", "PROPFIND":
				if !perms.HasRead() {
					http.Error(w, "Forbidden: Read permission required", http.StatusForbidden)
					return
				}
			case "PUT", "POST", "DELETE", "MKCOL", "COPY", "MOVE", "PROPPATCH", "PATCH":
				if !perms.HasWrite() {
					http.Error(w, "Forbidden: Write permission required", http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
