package model

import (
	"context"
)

type AuthHandler struct {
	ClientSecret   string
	ClientUUID     string
	IDX            bool
	GCPWorkstation string
}

func (a *AuthHandler) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	md := map[string]string{
		"client_secret": a.ClientSecret,
		"client_uuid":   a.ClientUUID,
	}
	// Only send when IDX is enabled and value is present.
	if a.IDX && a.GCPWorkstation != "" {
		md["gcp_workstation"] = a.GCPWorkstation
	}
	return md, nil
}

func (a *AuthHandler) RequireTransportSecurity() bool {
	return false
}
