package provider

import "strings"

const (
	maxAccountModelMetadataEntries  = 16
	maxAccountModelMetadataKeyLen   = 64
	maxAccountModelMetadataValueLen = 128
)

const (
	EndpointChatCompletions = "chat_completions"
	EndpointResponses       = "responses"
)

const (
	EndpointCapabilitySupported   = "supported"
	EndpointCapabilityUnsupported = "unsupported"
	EndpointCapabilityUnknown     = "unknown"
)

// NormalizeEndpoint returns the internal endpoint name used by routing and
// model capability metadata. An empty endpoint preserves the legacy model-only
// selection behavior.
func NormalizeEndpoint(endpoint string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "":
		return "", nil
	case EndpointChatCompletions, "chat", "/v1/chat/completions":
		return EndpointChatCompletions, nil
	case EndpointResponses, "/v1/responses":
		return EndpointResponses, nil
	default:
		return "", ErrInvalidInput
	}
}

func EndpointCapabilityMetadata(endpoint, capability string) map[string]string {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil || endpoint == "" {
		return nil
	}
	capability = normalizeEndpointCapability(capability)
	return map[string]string{"endpoint." + endpoint: capability}
}

func MergeEndpointCapabilityMetadata(metadata map[string]string, endpoint, capability string) map[string]string {
	merged := cloneStringMap(metadata)
	for key, value := range EndpointCapabilityMetadata(endpoint, capability) {
		if merged == nil {
			merged = map[string]string{}
		}
		merged[key] = value
	}
	return merged
}

// EndpointCapability reads both the canonical endpoint.<name> key and the
// short endpoint key used by early local configurations. Missing or malformed
// values are deliberately treated as unknown so an old account is not
// collectively disabled by a metadata migration.
func EndpointCapability(metadata map[string]string, endpoint string) string {
	endpoint, err := NormalizeEndpoint(endpoint)
	if err != nil || endpoint == "" {
		return EndpointCapabilityUnknown
	}
	for _, key := range []string{"endpoint." + endpoint, endpoint} {
		if capability := normalizeEndpointCapability(metadata[key]); capability != EndpointCapabilityUnknown || strings.TrimSpace(metadata[key]) != "" {
			return capability
		}
	}
	return EndpointCapabilityUnknown
}

func normalizeEndpointCapability(capability string) string {
	switch strings.ToLower(strings.TrimSpace(capability)) {
	case EndpointCapabilitySupported:
		return EndpointCapabilitySupported
	case EndpointCapabilityUnsupported:
		return EndpointCapabilityUnsupported
	default:
		return EndpointCapabilityUnknown
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func normalizeAccountModelMetadata(metadata map[string]string) (map[string]string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	if len(metadata) > maxAccountModelMetadataEntries {
		return nil, ErrInvalidInput
	}
	normalized := make(map[string]string, len(metadata))
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || len(key) > maxAccountModelMetadataKeyLen || len(value) > maxAccountModelMetadataValueLen {
			return nil, ErrInvalidInput
		}
		normalized[key] = value
	}
	return normalized, nil
}

func endpointCapabilityUnsupported(metadata map[string]string, endpoint string) bool {
	return EndpointCapability(metadata, endpoint) == EndpointCapabilityUnsupported
}
