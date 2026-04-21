package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	gcpMetadataHost = "http://metadata.google.internal"
)

var (
	zoneToRegionRe      = regexp.MustCompile(`-[a-z]$`)
	clusterIDFromScript = regexp.MustCompile(`workstation_cluster_id=([^, \n\r]+)`)
	errNotRunningOnGCP  = errors.New("gcp metadata not available")
)

// GetGCPWorkstationFullPath attempts to build a Workstations resource path using
// Google Cloud metadata server. It returns empty string when metadata is unavailable.
//
// Result example:
// projects/<PROJECT_NUM>/locations/<REGION>/workstationClusters/<CLUSTER_ID>/workstationConfigs/monospace-config/workstations/<WORKSTATION_ID>
func GetGCPWorkstationFullPath(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	projectNum, err := gcpMetaGet(ctx, "/computeMetadata/v1/project/numeric-project-id")
	if err != nil || projectNum == "" {
		return "", errNotRunningOnGCP
	}

	zonePath, err := gcpMetaGet(ctx, "/computeMetadata/v1/instance/zone")
	if err != nil || zonePath == "" {
		return "", errNotRunningOnGCP
	}
	zone := lastPathSegment(zonePath)
	region := zoneToRegionRe.ReplaceAllString(zone, "")
	if region == "" {
		return "", fmt.Errorf("unable to derive region from zone: %q", zone)
	}

	startupScript, err := gcpMetaGet(ctx, "/computeMetadata/v1/instance/attributes/startup-script")
	if err != nil || startupScript == "" {
		return "", fmt.Errorf("startup-script unavailable: %w", err)
	}
	clusterID := extractFirstGroup(clusterIDFromScript, startupScript)
	if clusterID == "" {
		return "", fmt.Errorf("workstation_cluster_id not found in startup-script")
	}

	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("hostname unavailable: %w", err)
	}
	workstationID := strings.Split(host, ".")[0]
	if workstationID == "" {
		return "", fmt.Errorf("invalid hostname: %q", host)
	}

	fullPath := fmt.Sprintf(
		"projects/%s/locations/%s/workstationClusters/%s/workstationConfigs/monospace-config/workstations/%s",
		strings.TrimSpace(projectNum),
		region,
		clusterID,
		workstationID,
	)
	return fullPath, nil
}

func gcpMetaGet(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gcpMetadataHost+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("metadata status %s", resp.Status)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func lastPathSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "/")
	return parts[len(parts)-1]
}

func extractFirstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
