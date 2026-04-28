package tunnel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
)

const Version = "7.5"

type Message struct {
	Type          string `json:"type"`
	Key           string `json:"key,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	RemotePort    int    `json:"remote_port,omitempty"`
	RequestedPort int    `json:"requested_port,omitempty"`
	Target        string `json:"target,omitempty"`
	ID            string `json:"id,omitempty"`
	Error         string `json:"error,omitempty"`
	Version       string `json:"version,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	RemoteAddr    string `json:"remote_addr,omitempty"`
	Payload       string `json:"payload,omitempty"`

	Subdomain  string            `json:"subdomain,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       []byte            `json:"body,omitempty"`
	StatusCode int               `json:"status_code,omitempty"`

	UDPSecret  string `json:"udp_secret,omitempty"`
	BaseDomain string `json:"base_domain,omitempty"`
}

func NewEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc
}

func NewDecoder(r io.Reader) *json.Decoder { return json.NewDecoder(r) }

func GenerateID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
