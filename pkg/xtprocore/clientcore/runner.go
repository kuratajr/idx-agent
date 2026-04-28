package clientcore

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nezhahq/agent/pkg/xtprocore/tunnel"
)

const (
	defaultHeartbeatInterval = 2 * time.Second
	defaultBackoffMin        = 3 * time.Second
	defaultBackoffMax        = 5 * time.Minute
)

func NewRunner(cfg Config) (*Runner, error) {
	if cfg.ServerAddr == "" {
		return nil, fmt.Errorf("server address is required")
	}
	if cfg.ClientID == "" {
		host, _ := os.Hostname()
		cfg.ClientID = "agent-" + host
	}
	if cfg.Protocol == "" {
		cfg.Protocol = ProtocolTCP
	}
	if cfg.Protocol == ProtocolFile && cfg.FileShare == nil {
		return nil, fmt.Errorf("file share config is required for file protocol")
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = defaultHeartbeatInterval
	}
	if cfg.ReconnectBackoffMin <= 0 {
		cfg.ReconnectBackoffMin = defaultBackoffMin
	}
	if cfg.ReconnectBackoffMax <= 0 {
		cfg.ReconnectBackoffMax = defaultBackoffMax
	}
	r := &Runner{cfg: cfg}
	r.status = RuntimeStatus{
		TunnelID:      cfg.TunnelID,
		State:         StatePending,
		Protocol:      cfg.Protocol,
		LocalAddr:     cfg.LocalAddr,
		RequestedPort: cfg.RequestedPort,
		UpdatedAt:     time.Now(),
	}
	return r, nil
}

func (r *Runner) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel

	if r.cfg.Protocol == ProtocolFile {
		share, err := startFileShareServer(r.cfg.FileShare)
		if err != nil {
			r.updateStatus(func(s *RuntimeStatus) { s.State, s.Error = StateError, err.Error() })
			return err
		}
		r.share = share
		r.cfg.LocalAddr = share.LocalAddr
		r.cfg.Protocol = ProtocolHTTP
	}

	backoff := r.cfg.ReconnectBackoffMin
	for {
		select {
		case <-ctx.Done():
			r.stop()
			return nil
		default:
		}

		rc := &runtimeClient{
			cfg:                r.cfg,
			serverAddr:         r.cfg.ServerAddr,
			localAddr:          r.cfg.LocalAddr,
			clientID:           r.cfg.ClientID,
			remotePort:         r.cfg.RequestedPort,
			protocol:           r.cfg.Protocol,
			certFingerprint:    strings.ToLower(strings.TrimSpace(r.cfg.CertFingerprint)),
			insecureSkipVerify: r.cfg.InsecureSkipVerify,
			udpSessions:        make(map[string]*udpClientSession),
		}
		r.client = rc
		r.updateStatus(func(s *RuntimeStatus) {
			s.State = StateConnecting
			s.Error = ""
			s.Protocol = r.cfg.Protocol
			s.LocalAddr = r.cfg.LocalAddr
			s.RequestedPort = r.cfg.RequestedPort
		})

		if err := rc.connectControl(r); err != nil {
			r.updateStatus(func(s *RuntimeStatus) { s.State, s.Error = StateError, err.Error() })
			if !sleepContext(ctx, backoff) {
				r.stop()
				return nil
			}
			backoff *= 2
			if backoff > r.cfg.ReconnectBackoffMax {
				backoff = r.cfg.ReconnectBackoffMax
			}
			continue
		}

		backoff = r.cfg.ReconnectBackoffMin
		err := rc.receiveLoop(ctx, r)
		rc.closeControl()
		if ctx.Err() != nil {
			r.stop()
			return nil
		}
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			r.updateStatus(func(s *RuntimeStatus) { s.State, s.Error = StateDegraded, err.Error() })
		}
		if !sleepContext(ctx, backoff) {
			r.stop()
			return nil
		}
	}
}

func (r *Runner) Close() {
	r.cancelOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.stop()
	})
}

func (r *Runner) Status() RuntimeStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.status
}

func (r *Runner) updateStatus(update func(*RuntimeStatus)) {
	r.mu.Lock()
	update(&r.status)
	r.status.UpdatedAt = time.Now()
	s := r.status
	r.mu.Unlock()
	if r.cfg.OnStatus != nil {
		r.cfg.OnStatus(s)
	}
}

func (r *Runner) emitEvent(kind, msg string) {
	if r.cfg.OnEvent == nil {
		return
	}
	status := r.Status()
	publicURL := status.PublicAddr
	if status.Subdomain != "" && status.BaseDomain != "" {
		publicURL = "https://" + status.Subdomain + "." + status.BaseDomain
	}
	r.cfg.OnEvent(Event{TunnelID: r.cfg.TunnelID, Kind: kind, Message: msg, PublicURL: publicURL, CreatedAt: time.Now()})
}

func (r *Runner) stop() {
	if r.client != nil {
		r.client.closeControl()
	}
	if r.share != nil {
		_ = r.share.Close(context.Background())
	}
	r.updateStatus(func(s *RuntimeStatus) { s.State = StateStopped })
}

func (c *runtimeClient) connectControl(r *Runner) error {
	conn, err := tls.Dial("tcp", c.serverAddr, c.buildTLSConfig())
	if err != nil {
		return err
	}
	c.closeOnce = sync.Once{}
	c.done = make(chan struct{})
	c.control = conn
	c.enc = &jsonWriter{enc: newTunnelEncoder(conn)}
	c.dec = &jsonReader{dec: newTunnelDecoder(bufio.NewReader(conn))}

	register := tunnel.Message{Type: "register", Key: c.key, ClientID: c.clientID, Target: c.localAddr, Protocol: string(c.protocol)}
	if c.remotePort > 0 {
		register.RequestedPort = c.remotePort
	}
	if err := c.enc.Encode(register); err != nil {
		return err
	}
	var resp tunnel.Message
	if err := c.dec.Decode(&resp); err != nil {
		return err
	}
	if resp.Type != "registered" {
		return fmt.Errorf("registration failed: %+v", resp)
	}
	if strings.TrimSpace(resp.Key) != "" {
		c.key = strings.TrimSpace(resp.Key)
	}
	c.remotePort = resp.RemotePort
	if strings.TrimSpace(resp.Protocol) != "" {
		c.protocol = Protocol(strings.ToLower(strings.TrimSpace(resp.Protocol)))
	}
	c.subdomain = resp.Subdomain
	c.baseDomain = resp.BaseDomain
	if resp.UDPSecret != "" {
		secret, err := base64.StdEncoding.DecodeString(resp.UDPSecret)
		if err == nil && len(secret) == 32 {
			c.udpSecret = secret
		}
	}
	hostPart := c.serverAddr
	if host, _, err := net.SplitHostPort(c.serverAddr); err == nil {
		hostPart = host
	}
	c.publicHost = net.JoinHostPort(hostPart, strconv.Itoa(c.remotePort))

	r.updateStatus(func(s *RuntimeStatus) {
		s.State = StateActive
		s.Protocol = c.protocol
		s.AssignedRemotePort = c.remotePort
		s.PublicAddr = c.publicHost
		s.Subdomain = c.subdomain
		s.BaseDomain = c.baseDomain
		s.LastHeartbeat = time.Now()
		s.Error = ""
	})
	r.emitEvent("registered", "tunnel registered")

	if c.protocol == ProtocolUDP {
		_ = c.setupUDPChannel()
	}
	go c.heartbeatLoop(r)
	return nil
}

func (c *runtimeClient) receiveLoop(ctx context.Context, r *Runner) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var msg tunnel.Message
		if err := c.dec.Decode(&msg); err != nil {
			if isEOF(err) {
				return io.EOF
			}
			return err
		}
		r.updateStatus(func(s *RuntimeStatus) {
			s.BytesUp = atomic.LoadUint64(&c.bytesUp)
			s.BytesDown = atomic.LoadUint64(&c.bytesDown)
			s.ActiveSessions = atomic.LoadInt64(&c.activeSessions)
			s.TotalSessions = atomic.LoadUint64(&c.totalSessions) + atomic.LoadUint64(&c.totalUDPSessions)
			s.LastHeartbeat = time.Now()
		})
		switch msg.Type {
		case "proxy":
			go c.handleProxy(msg.ID)
		case "udp_open":
			c.handleUDPOpen(msg)
		case "udp_close":
			c.handleUDPClose(msg.ID)
		case "ping":
			_ = c.enc.Encode(tunnel.Message{Type: "pong"})
		case "pong":
			c.recordPingReply()
		case "http_request":
			go c.handleHTTPRequest(msg)
		case "error":
			r.updateStatus(func(s *RuntimeStatus) { s.State, s.Error = StateError, msg.Error })
			r.emitEvent("server_error", msg.Error)
		}
	}
}

func (c *runtimeClient) heartbeatLoop(r *Runner) {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = c.enc.Encode(tunnel.Message{Type: "ping"})
		case <-c.done:
			return
		}
	}
}

func (c *runtimeClient) handleProxy(id string) {
	if c.protocol == ProtocolUDP || strings.TrimSpace(id) == "" {
		return
	}
	localConn, err := net.Dial("tcp", c.localAddr)
	if err != nil {
		_ = c.enc.Encode(tunnel.Message{Type: "proxy_error", ID: id, Error: err.Error()})
		return
	}
	atomic.AddInt64(&c.activeSessions, 1)
	atomic.AddUint64(&c.totalSessions, 1)

	srvConn, err := tls.Dial("tcp", c.serverAddr, c.buildTLSConfig())
	if err != nil {
		localConn.Close()
		_ = c.enc.Encode(tunnel.Message{Type: "proxy_error", ID: id, Error: err.Error()})
		return
	}
	enc := newTunnelEncoder(srvConn)
	if err := enc.Encode(tunnel.Message{Type: "proxy", Key: c.key, ClientID: c.clientID, ID: id}); err != nil {
		localConn.Close()
		srvConn.Close()
		return
	}
	go func() {
		defer atomic.AddInt64(&c.activeSessions, -1)
		proxyCopyCount(srvConn, localConn, &c.bytesUp)
	}()
	go proxyCopyCount(localConn, srvConn, &c.bytesDown)
}

func (c *runtimeClient) closeControl() {
	c.closeOnce.Do(func() {
		if c.done != nil {
			close(c.done)
		}
	})
	c.closeAllUDPSessions()
	c.stopUDPPing()
	c.udpMu.Lock()
	if c.udpConn != nil {
		c.udpConn.Close()
		c.udpConn = nil
	}
	c.udpReady = false
	c.udpMu.Unlock()
	if c.control != nil {
		c.control.Close()
	}
	c.control = nil
	c.enc = nil
	c.dec = nil
}

func proxyCopyCount(dst, src net.Conn, counter *uint64) {
	defer dst.Close()
	defer src.Close()
	reader := io.TeeReader(src, &byteCounter{counter: counter})
	_, _ = io.Copy(dst, reader)
}

func (c *runtimeClient) recordPingReply() {
	sent := atomic.SwapInt64(&c.pingSent, 0)
	if sent <= 0 {
		return
	}
	atomic.StoreInt64(&c.pingMs, time.Since(time.Unix(0, sent)).Milliseconds())
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection")
}
