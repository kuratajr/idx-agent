package clientcore

import (
	"encoding/json"
	"io"
	"sync/atomic"
	"time"

	"github.com/nezhahq/agent/pkg/xtprocore/tunnel"
)

const (
	backendIdleTimeout = 5 * time.Second
	backendIdleRetries = 3
	udpControlInterval = 2 * time.Second
	udpControlTimeout  = 6 * time.Second
)

const (
	udpMsgHandshake byte = 1
	udpMsgData      byte = 2
	udpMsgClose     byte = 3
	udpMsgPing      byte = 4
	udpMsgPong      byte = 5
)

type tunnelEncoder = json.Encoder
type tunnelDecoder = json.Decoder

func newTunnelEncoder(w io.Writer) *tunnelEncoder { return tunnel.NewEncoder(w) }
func newTunnelDecoder(r io.Reader) *tunnelDecoder { return tunnel.NewDecoder(r) }

func (w *jsonWriter) Encode(msg tunnel.Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(msg)
}

func (r *jsonReader) Decode(msg *tunnel.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dec.Decode(msg)
}

func (s *udpClientSession) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.timer != nil {
			s.timer.Stop()
		}
		if s.conn != nil {
			s.conn.Close()
		}
	})
}

type byteCounter struct{ counter *uint64 }

func (b *byteCounter) Write(p []byte) (int, error) {
	if len(p) > 0 && b.counter != nil {
		atomic.AddUint64(b.counter, uint64(len(p)))
	}
	return len(p), nil
}
