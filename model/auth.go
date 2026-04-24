package model

import (
	"context"
)

type AuthHandler struct {
	ClientSecret   string
	ClientUUID     string
	ClientName     string
	IDX            bool
	GCPWorkstation string
}

func (a *AuthHandler) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	md := map[string]string{
		"client_secret": a.ClientSecret,
		"client_uuid":   a.ClientUUID,
	}
	if a.ClientName != "" {
		md["client_name"] = a.ClientName
	}
	// IDX bootstrap metadata (used by server for rules matching).
	// Keep keys lowercase for gRPC metadata canonicalization.
	if a.IDX {
		md["x-idx"] = "true"
		if a.ClientName != "" {
			// In IDX mode, ClientName resolves to WORKSPACE_SLUG (or hostname fallback).
			md["x-workspace-slug"] = a.ClientName
		}
	} else {
		md["x-idx"] = "false"
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
