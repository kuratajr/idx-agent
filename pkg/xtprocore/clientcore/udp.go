package clientcore

import (
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"time"

	"github.com/nezhahq/agent/pkg/xtprocore/tunnel"
)

func (c *runtimeClient) handleUDPOpen(msg tunnel.Message) {
	if c.protocol != ProtocolUDP || msg.ID == "" {
		return
	}
	backend, err := net.ResolveUDPAddr("udp", c.localAddr)
	if err != nil {
		c.sendUDPClose(msg.ID)
		return
	}
	conn, err := net.DialUDP("udp", nil, backend)
	if err != nil {
		c.sendUDPClose(msg.ID)
		return
	}
	sess := &udpClientSession{id: msg.ID, conn: conn, closed: make(chan struct{})}
	c.udpMu.Lock()
	if old, ok := c.udpSessions[msg.ID]; ok {
		delete(c.udpSessions, msg.ID)
		old.Close()
	}
	c.udpSessions[msg.ID] = sess
	atomic.AddUint64(&c.totalUDPSessions, 1)
	c.udpMu.Unlock()
	go c.readFromUDPLocal(sess)
}

func (c *runtimeClient) handleUDPClose(id string) {
	if id == "" {
		return
	}
	c.removeUDPSession(id, false)
}

func (c *runtimeClient) setupUDPChannel() error {
	addr, err := net.ResolveUDPAddr("udp", c.serverAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return err
	}
	c.udpMu.Lock()
	if c.udpConn != nil {
		c.udpConn.Close()
	}
	c.udpConn = conn
	c.udpReady = false
	c.udpMu.Unlock()
	go c.readUDPControl(conn)
	_ = c.sendUDPHandshake()
	go c.udpHandshakeRetry()
	return nil
}

func (c *runtimeClient) readUDPControl(conn *net.UDPConn) {
	defer c.stopUDPPing()
	buf := make([]byte, 65535)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		c.handleUDPControlPacket(packet)
	}
}

func (c *runtimeClient) handleUDPControlPacket(packet []byte) {
	if len(packet) < 3 {
		return
	}
	msgType := packet[0]
	key, idx, ok := decodeUDPField(packet, 1)
	if !ok || key == "" || key != c.key {
		return
	}
	switch msgType {
	case udpMsgData:
		id, next, ok := decodeUDPField(packet, idx)
		if !ok || id == "" {
			return
		}
		c.handleUDPDataPacket(id, append([]byte(nil), packet[next:]...))
	case udpMsgClose:
		id, _, ok := decodeUDPField(packet, idx)
		if ok && id != "" {
			c.handleUDPClose(id)
		}
	case udpMsgHandshake:
		c.udpMu.Lock()
		c.udpReady = true
		c.udpMu.Unlock()
		c.startUDPPing()
	case udpMsgPong:
		c.udpCtrlMu.Lock()
		c.udpLastPong = time.Now()
		c.udpCtrlMu.Unlock()
	case udpMsgPing:
		_, next, ok := decodeUDPField(packet, idx)
		if !ok {
			return
		}
		c.sendUDPPong(append([]byte(nil), packet[next:]...))
	}
}

func (c *runtimeClient) handleUDPDataPacket(id string, payload []byte) {
	if len(payload) == 0 {
		return
	}
	if c.udpSecret != nil {
		decrypted, err := tunnel.DecryptUDP(c.udpSecret, payload)
		if err != nil {
			return
		}
		payload = decrypted
	}
	sess := c.getUDPSession(id)
	if sess == nil {
		return
	}
	if _, err := sess.conn.Write(payload); err != nil {
		c.removeUDPSession(id, true)
		return
	}
	c.startBackendWait(id)
	atomic.AddUint64(&c.bytesDown, uint64(len(payload)))
}

func (c *runtimeClient) readFromUDPLocal(sess *udpClientSession) {
	buf := make([]byte, 65535)
	for {
		n, err := sess.conn.Read(buf)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}
		payload := append([]byte(nil), buf[:n]...)
		c.cancelBackendWait(sess.id)
		c.sendUDPData(sess.id, payload)
	}
	c.removeUDPSession(sess.id, true)
}

func (c *runtimeClient) getUDPSession(id string) *udpClientSession {
	c.udpMu.Lock()
	defer c.udpMu.Unlock()
	return c.udpSessions[id]
}

func (c *runtimeClient) removeUDPSession(id string, notify bool) {
	c.udpMu.Lock()
	sess := c.udpSessions[id]
	if sess != nil {
		delete(c.udpSessions, id)
	}
	c.udpMu.Unlock()
	if sess == nil {
		return
	}
	sess.Close()
	if notify {
		c.sendUDPClose(id)
	}
}

func (c *runtimeClient) closeAllUDPSessions() {
	c.udpMu.Lock()
	sessions := make([]*udpClientSession, 0, len(c.udpSessions))
	for _, sess := range c.udpSessions {
		sessions = append(sessions, sess)
	}
	c.udpSessions = make(map[string]*udpClientSession)
	c.udpMu.Unlock()
	for _, sess := range sessions {
		sess.Close()
	}
}

func (c *runtimeClient) startBackendWait(id string) {
	c.udpMu.Lock()
	defer c.udpMu.Unlock()
	if sess, ok := c.udpSessions[id]; ok {
		if sess.timer != nil {
			sess.timer.Stop()
		}
		sess.idleCount = 0
		sess.timer = time.AfterFunc(backendIdleTimeout, func() { c.handleBackendTimeout(id) })
	}
}

func (c *runtimeClient) cancelBackendWait(id string) {
	c.udpMu.Lock()
	defer c.udpMu.Unlock()
	if sess, ok := c.udpSessions[id]; ok && sess.timer != nil {
		sess.timer.Stop()
		sess.timer = nil
		sess.idleCount = 0
	}
}

func (c *runtimeClient) handleBackendTimeout(id string) {
	sess := c.getUDPSession(id)
	if sess != nil {
		sess.idleCount++
		if sess.idleCount < backendIdleRetries {
			c.startBackendWait(id)
			return
		}
	}
	if c.enc != nil {
		_ = c.enc.Encode(tunnel.Message{Type: "udp_idle", ID: id, Protocol: "udp"})
	}
	c.removeUDPSession(id, true)
}

func (c *runtimeClient) sendUDPData(id string, payload []byte) {
	if len(payload) == 0 {
		return
	}
	if c.udpSecret != nil {
		encrypted, err := tunnel.EncryptUDP(c.udpSecret, payload)
		if err != nil {
			return
		}
		payload = encrypted
	}
	if err := c.writeUDP(udpMsgData, id, payload); err == nil {
		atomic.AddUint64(&c.bytesUp, uint64(len(payload)))
	}
}

func (c *runtimeClient) sendUDPClose(id string) {
	_ = c.writeUDP(udpMsgClose, id, nil)
	if c.enc != nil {
		_ = c.enc.Encode(tunnel.Message{Type: "udp_close", ID: id, Protocol: "udp"})
	}
}

func (c *runtimeClient) sendUDPHandshake() error          { return c.writeUDP(udpMsgHandshake, "", nil) }
func (c *runtimeClient) sendUDPPing(payload []byte) error { return c.writeUDP(udpMsgPing, "", payload) }
func (c *runtimeClient) sendUDPPong(payload []byte)       { _ = c.writeUDP(udpMsgPong, "", payload) }

func (c *runtimeClient) udpHandshakeRetry() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for {
		c.udpMu.Lock()
		ready := c.udpReady
		connPresent := c.udpConn != nil
		c.udpMu.Unlock()
		if ready || !connPresent {
			return
		}
		select {
		case <-ticker.C:
			_ = c.sendUDPHandshake()
		case <-timeout.C:
			return
		case <-c.done:
			return
		}
	}
}

func (c *runtimeClient) writeUDP(msgType byte, id string, payload []byte) error {
	c.udpMu.Lock()
	conn := c.udpConn
	key := c.key
	c.udpMu.Unlock()
	if conn == nil {
		return errors.New("udp not ready")
	}
	buf := buildUDPMessage(msgType, key, id, payload)
	_, err := conn.Write(buf)
	return err
}

func (c *runtimeClient) startUDPPing() {
	c.udpCtrlMu.Lock()
	if c.udpPingTicker != nil {
		c.udpCtrlMu.Unlock()
		return
	}
	ticker := time.NewTicker(udpControlInterval)
	stopCh := make(chan struct{})
	c.udpPingTicker = ticker
	c.udpPingStop = stopCh
	c.udpLastPong = time.Now()
	c.udpCtrlMu.Unlock()
	go c.udpPingLoop(ticker, stopCh)
}

func (c *runtimeClient) stopUDPPing() {
	c.udpCtrlMu.Lock()
	if c.udpPingTicker != nil {
		c.udpPingTicker.Stop()
		c.udpPingTicker = nil
	}
	if c.udpPingStop != nil {
		close(c.udpPingStop)
		c.udpPingStop = nil
	}
	c.udpCtrlMu.Unlock()
}

func (c *runtimeClient) udpPingLoop(ticker *time.Ticker, stopCh chan struct{}) {
	for {
		select {
		case <-ticker.C:
			payload := make([]byte, 8)
			binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))
			_ = c.sendUDPPing(payload)
			c.checkUDPPingTimeout()
		case <-stopCh:
			return
		case <-c.done:
			return
		}
	}
}

func (c *runtimeClient) checkUDPPingTimeout() {
	c.udpCtrlMu.Lock()
	last := c.udpLastPong
	c.udpCtrlMu.Unlock()
	if time.Since(last) > udpControlTimeout {
		c.closeAllUDPSessions()
	}
}

func decodeUDPField(packet []byte, offset int) (string, int, bool) {
	if offset+2 > len(packet) {
		return "", offset, false
	}
	l := int(binary.BigEndian.Uint16(packet[offset : offset+2]))
	offset += 2
	if offset+l > len(packet) {
		return "", offset, false
	}
	return string(packet[offset : offset+l]), offset + l, true
}

func buildUDPMessage(msgType byte, key, id string, payload []byte) []byte {
	keyLen := len(key)
	idLen := len(id)
	total := 1 + 2 + keyLen
	if msgType != udpMsgHandshake {
		total += 2 + idLen
	}
	total += len(payload)
	buf := make([]byte, total)
	buf[0] = msgType
	binary.BigEndian.PutUint16(buf[1:], uint16(keyLen))
	copy(buf[3:], key)
	offset := 3 + keyLen
	if msgType != udpMsgHandshake {
		binary.BigEndian.PutUint16(buf[offset:], uint16(idLen))
		offset += 2
		copy(buf[offset:], id)
		offset += idLen
	}
	copy(buf[offset:], payload)
	return buf
}
