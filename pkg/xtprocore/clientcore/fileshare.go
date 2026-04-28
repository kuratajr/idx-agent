package clientcore

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/nezhahq/agent/pkg/xtprocore/fileserver"
)

type fileShareServer struct {
	LocalAddr string
	server    *http.Server
}

func startFileShareServer(cfg *FileShareConfig) (*fileShareServer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing file share config")
	}
	path := fileserver.NormalizePath(cfg.Path)
	if !fileserver.PathExists(path) || !fileserver.IsDirectory(path) {
		return nil, fmt.Errorf("invalid file share path: %s", cfg.Path)
	}
	webdavServer, err := fileserver.NewWebDAVServer(path, "/webdav", cfg.Username, cfg.Password, cfg.Permissions)
	if err != nil {
		return nil, err
	}
	port, err := findFreePort()
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/webdav", webdavServer)
	mux.Handle("/webdav/", webdavServer)
	server := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", port),
		Handler: mux,
	}
	share := &fileShareServer{LocalAddr: server.Addr, server: server}
	go func() { _ = server.ListenAndServe() }()
	return share, nil
}

func (s *fileShareServer) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Shutdown(ctx)
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
