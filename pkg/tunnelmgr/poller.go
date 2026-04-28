package tunnelmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nezhahq/agent/model"
)

type RemotePoller struct {
	client *http.Client
}

func NewRemotePoller(timeout time.Duration) *RemotePoller {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &RemotePoller{
		client: &http.Client{Timeout: timeout},
	}
}

func (p *RemotePoller) FetchDesiredState(ctx context.Context, endpoint, token, nodeID string) (model.TunnelDesiredState, error) {
	if strings.TrimSpace(endpoint) == "" {
		return model.TunnelDesiredState{}, fmt.Errorf("missing tunnel control url")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return model.TunnelDesiredState{}, err
	}
	q := u.Query()
	q.Set("node_id", nodeID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return model.TunnelDesiredState{}, err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return model.TunnelDesiredState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return model.TunnelDesiredState{}, fmt.Errorf("tunnel control returned %s", resp.Status)
	}
	var payload struct {
		Success bool                     `json:"success"`
		Data    model.TunnelDesiredState `json:"data"`
		Error   string                   `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.TunnelDesiredState{}, err
	}
	if !payload.Success {
		if payload.Error == "" {
			payload.Error = "remote tunnel control request failed"
		}
		return model.TunnelDesiredState{}, errors.New(payload.Error)
	}
	return payload.Data, nil
}

func (p *RemotePoller) PushSnapshot(ctx context.Context, endpoint, token, nodeID string, snapshot model.TunnelStatusSnapshot) error {
	if strings.TrimSpace(endpoint) == "" {
		return nil
	}
	body := map[string]interface{}{
		"node_id": nodeID,
		"version": snapshot.Version,
		"tunnels": snapshot.Tunnels,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tunnel status push returned %s", resp.Status)
	}
	return nil
}
