package clientcore

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/nezhahq/agent/pkg/xtprocore/fileserver"
)

type Protocol string

const (
	ProtocolTCP  Protocol = "tcp"
	ProtocolUDP  Protocol = "udp"
	ProtocolHTTP Protocol = "http"
	ProtocolFile Protocol = "file"
)

type TunnelState string

const (
	StatePending    TunnelState = "pending"
	StateConnecting TunnelState = "connecting"
	StateActive     TunnelState = "active"
	StateDegraded   TunnelState = "degraded"
	StateError      TunnelState = "error"
	StateStopped    TunnelState = "stopped"
)

type FileShareConfig struct {
	Path        string
	Username    string
	Password    string
	Permissions fileserver.Permissions
}

type Config struct {
	TunnelID            string
	ServerAddr          string
	ClientID            string
	LocalAddr           string
	Protocol            Protocol
	RequestedPort       int
	CertFingerprint     string
	InsecureSkipVerify  bool
	FileShare           *FileShareConfig
	OnStatus            func(RuntimeStatus)
	OnEvent             func(Event)
	HeartbeatInterval   time.Duration
	ReconnectBackoffMin time.Duration
	ReconnectBackoffMax time.Duration
}

type Event struct {
	TunnelID  string
	Kind      string
	Message   string
	PublicURL string
	CreatedAt time.Time
}

type RuntimeStatus struct {
	TunnelID           string
	State              TunnelState
	Protocol           Protocol
	LocalAddr          string
	RequestedPort      int
	AssignedRemotePort int
	PublicAddr         string
	Subdomain          string
	BaseDomain         string
	Error              string
	BytesUp            uint64
	BytesDown          uint64
	ActiveSessions     int64
	TotalSessions      uint64
	LastHeartbeat      time.Time
	UpdatedAt          time.Time
}

type Runner struct {
	cfg        Config
	mu         sync.RWMutex
	status     RuntimeStatus
	share      *fileShareServer
	client     *runtimeClient
	cancel     context.CancelFunc
	cancelOnce sync.Once
}

type runtimeClient struct {
	cfg                Config
	serverAddr         string
	localAddr          string
	key                string
	clientID           string
	remotePort         int
	publicHost         string
	protocol           Protocol
	subdomain          string
	baseDomain         string
	certFingerprint    string
	insecureSkipVerify bool

	control   net.Conn
	enc       *jsonWriter
	dec       *jsonReader
	closeOnce sync.Once
	done      chan struct{}

	bytesUp        uint64
	bytesDown      uint64
	pingSent       int64
	pingMs         int64
	activeSessions int64
	totalSessions  uint64

	udpMu       sync.Mutex
	udpSessions map[string]*udpClientSession
	udpConn     *net.UDPConn
	udpReady    bool

	udpCtrlMu     sync.Mutex
	udpPingTicker *time.Ticker
	udpPingStop   chan struct{}
	udpLastPong   time.Time

	dataMu           sync.Mutex
	totalUDPSessions uint64
	udpSecret        []byte
}

type jsonWriter struct {
	enc *tunnelEncoder
	mu  sync.Mutex
}

type jsonReader struct {
	dec *tunnelDecoder
	mu  sync.Mutex
}

type udpClientSession struct {
	id        string
	conn      *net.UDPConn
	closeOnce sync.Once
	closed    chan struct{}
	timer     *time.Timer
	idleCount int
}
