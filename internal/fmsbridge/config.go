package fmsbridge

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envFMSBaseURL          = "FMS_BASE_URL"
	envFMSServiceToken     = "FMS_SERVICE_TOKEN"
	envFMSPageSize         = "FMS_PAGE_SIZE"
	envFMSArtifactPageSize = "FMS_ARTIFACT_PAGE_SIZE"
	envFMSTimeout          = "FMS_REQUEST_TIMEOUT"
)

// ConfigFromEnvironment leaves the bridge disabled when FMS_BASE_URL is not
// set. This keeps upstream WeKnora deployments unchanged until an operator
// explicitly enables the FMS publishing source profile.
func ConfigFromEnvironment() (Config, bool, error) {
	return configFromLookup(os.Getenv)
}

func configFromLookup(lookup func(string) string) (Config, bool, error) {
	baseURL := strings.TrimSpace(lookup(envFMSBaseURL))
	if baseURL == "" {
		return Config{}, false, nil
	}
	config := Config{BaseURL: baseURL, ServiceToken: strings.TrimSpace(lookup(envFMSServiceToken))}
	if raw := strings.TrimSpace(lookup(envFMSPageSize)); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return Config{}, false, fmt.Errorf("%s must be an integer from 1 to 100", envFMSPageSize)
		}
		config.PageSize = value
	}
	if raw := strings.TrimSpace(lookup(envFMSArtifactPageSize)); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000 {
			return Config{}, false, fmt.Errorf("%s must be an integer from 1 to 1000", envFMSArtifactPageSize)
		}
		config.ArtifactPageSize = value
	}
	if raw := strings.TrimSpace(lookup(envFMSTimeout)); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil || value <= 0 {
			return Config{}, false, fmt.Errorf("%s must be a positive Go duration", envFMSTimeout)
		}
		config.Timeout = value
	}
	normalized, err := config.normalized()
	if err != nil {
		return Config{}, false, err
	}
	return normalized, true, nil
}
