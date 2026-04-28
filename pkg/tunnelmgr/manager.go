package tunnelmgr

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/nezhahq/agent/pkg/xtprocore/clientcore"
	"github.com/nezhahq/agent/pkg/xtprocore/fileserver"

	"github.com/nezhahq/agent/model"
)

type Manager struct {
	mu          sync.RWMutex
	instances   map[string]*instance
	lastVersion string
}

type instance struct {
	spec   model.TunnelSpec
	runner *clientcore.Runner
	status model.TunnelStatus
	cancel context.CancelFunc
}

func New() *Manager {
	return &Manager{instances: make(map[string]*instance)}
}

func (m *Manager) ApplyDesiredState(state model.TunnelDesiredState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	desired := make(map[string]model.TunnelSpec, len(state.Tunnels))
	for _, spec := range state.Tunnels {
		if spec.ID == "" {
			return fmt.Errorf("tunnel id is required")
		}
		desired[spec.ID] = spec
	}

	if state.Replace {
		for id, inst := range m.instances {
			if _, ok := desired[id]; !ok {
				inst.cancel()
				delete(m.instances, id)
			}
		}
	}

	for id, spec := range desired {
		current, ok := m.instances[id]
		if !ok {
			if err := m.startLocked(spec); err != nil {
				return err
			}
			continue
		}
		if !spec.Enabled {
			current.cancel()
			current.status.State = model.TunnelStateStopped
			current.status.UpdatedAt = time.Now()
			delete(m.instances, id)
			continue
		}
		if specsEqual(current.spec, spec) {
			continue
		}
		current.cancel()
		delete(m.instances, id)
		if err := m.startLocked(spec); err != nil {
			return err
		}
	}

	m.lastVersion = state.Version
	return nil
}

func (m *Manager) Snapshot() model.TunnelStatusSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]model.TunnelStatus, 0, len(m.instances))
	for _, inst := range m.instances {
		statuses = append(statuses, inst.status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })
	return model.TunnelStatusSnapshot{
		Version: m.lastVersion,
		Tunnels: statuses,
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, inst := range m.instances {
		inst.cancel()
		delete(m.instances, id)
	}
}

func (m *Manager) startLocked(spec model.TunnelSpec) error {
	if !spec.Enabled {
		return nil
	}
	cfg, err := buildRunnerConfig(spec)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	inst := &instance{
		spec:   spec,
		cancel: cancel,
		status: model.TunnelStatus{
			ID:            spec.ID,
			Name:          spec.Name,
			Protocol:      spec.Protocol,
			State:         model.TunnelStatePending,
			LocalAddr:     spec.LocalAddr(),
			RequestedPort: spec.RequestedPort,
			UpdatedAt:     time.Now(),
		},
	}
	cfg.OnStatus = func(status clientcore.RuntimeStatus) {
		m.mu.Lock()
		defer m.mu.Unlock()
		current, ok := m.instances[spec.ID]
		if !ok {
			return
		}
		current.status = convertStatus(spec, status)
	}
	runner, err := clientcore.NewRunner(cfg)
	if err != nil {
		cancel()
		return err
	}
	inst.runner = runner
	m.instances[spec.ID] = inst
	go func() {
		_ = runner.Run(ctx)
	}()
	return nil
}

func buildRunnerConfig(spec model.TunnelSpec) (clientcore.Config, error) {
	cfg := clientcore.Config{
		TunnelID:           spec.ID,
		ServerAddr:         spec.ServerAddr,
		ClientID:           spec.ClientID,
		LocalAddr:          spec.LocalAddr(),
		Protocol:           clientcore.Protocol(spec.Protocol),
		RequestedPort:      spec.RequestedPort,
		InsecureSkipVerify: spec.InsecureSkipVerify,
		CertFingerprint:    spec.CertFingerprint,
	}
	if spec.Protocol == model.TunnelProtocolFile {
		if spec.FileShare == nil {
			return clientcore.Config{}, fmt.Errorf("file_share config is required for tunnel %s", spec.ID)
		}
		cfg.Protocol = clientcore.ProtocolFile
		cfg.FileShare = &clientcore.FileShareConfig{
			Path:        spec.FileShare.Path,
			Username:    spec.FileShare.Username,
			Password:    spec.FileShare.Password,
			Permissions: fileserver.ParsePermissions(spec.FileShare.Permissions),
		}
	}
	return cfg, nil
}

func convertStatus(spec model.TunnelSpec, status clientcore.RuntimeStatus) model.TunnelStatus {
	out := model.TunnelStatus{
		ID:                 spec.ID,
		Name:               spec.Name,
		Protocol:           spec.Protocol,
		State:              model.TunnelState(status.State),
		LocalAddr:          status.LocalAddr,
		RequestedPort:      status.RequestedPort,
		AssignedRemotePort: status.AssignedRemotePort,
		PublicAddr:         status.PublicAddr,
		Subdomain:          status.Subdomain,
		BaseDomain:         status.BaseDomain,
		Error:              status.Error,
		BytesUp:            status.BytesUp,
		BytesDown:          status.BytesDown,
		ActiveSessions:     status.ActiveSessions,
		TotalSessions:      status.TotalSessions,
		UpdatedAt:          status.UpdatedAt,
	}
	if !status.LastHeartbeat.IsZero() {
		hb := status.LastHeartbeat
		out.LastHeartbeat = &hb
	}
	return out
}

func specsEqual(a, b model.TunnelSpec) bool {
	if a.ID != b.ID || a.Name != b.Name || a.ServerAddr != b.ServerAddr || a.ClientID != b.ClientID || a.Protocol != b.Protocol ||
		a.LocalHost != b.LocalHost || a.LocalPort != b.LocalPort || a.RequestedPort != b.RequestedPort ||
		a.Enabled != b.Enabled || a.InsecureSkipVerify != b.InsecureSkipVerify || a.CertFingerprint != b.CertFingerprint {
		return false
	}
	return maps.EqualFunc(mapifyFile(a.FileShare), mapifyFile(b.FileShare), func(x, y string) bool { return x == y })
}

func mapifyFile(spec *model.TunnelFileShareSpec) map[string]string {
	if spec == nil {
		return nil
	}
	return map[string]string{
		"path":        spec.Path,
		"username":    spec.Username,
		"password":    spec.Password,
		"permissions": spec.Permissions,
	}
}
