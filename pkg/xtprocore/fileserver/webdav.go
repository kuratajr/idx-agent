package fileserver

import (
	"fmt"
	"net/http"

	"golang.org/x/net/webdav"
)

type WebDAVServer struct {
	handler *webdav.Handler
	root    string
	auth    BasicAuth
	perms   Permissions
}

func NewWebDAVServer(root, prefix, username, password string, perms Permissions) (*WebDAVServer, error) {
	root = NormalizePath(root)
	if !PathExists(root) {
		return nil, fmt.Errorf("path does not exist: %s", root)
	}
	if !IsDirectory(root) {
		return nil, fmt.Errorf("path is not a directory: %s", root)
	}
	handler := &webdav.Handler{
		Prefix:     prefix,
		FileSystem: webdav.Dir(root),
		LockSystem: webdav.NewMemLS(),
	}
	return &WebDAVServer{
		handler: handler,
		root:    root,
		auth:    BasicAuth{Username: username, Password: password},
		perms:   perms,
	}, nil
}

func (s *WebDAVServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := BasicAuthMiddleware(s.auth)(PermissionMiddleware(s.perms)(s.handler))
	h.ServeHTTP(w, r)
}

func (s *WebDAVServer) Root() string { return s.root }
