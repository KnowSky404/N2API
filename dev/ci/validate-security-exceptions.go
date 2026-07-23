package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultRegistryPath  = "security/exceptions.json"
	maxExceptionDuration = 30 * 24 * time.Hour
)

var (
	cvePattern           = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,}$`)
	identityTokenPattern = regexp.MustCompile(`^[A-Za-z0-9@._:/+~-]+$`)
	ownerPattern         = regexp.MustCompile(`^[A-Za-z0-9@._/-]+$`)
)

type exceptionRegistry struct {
	Version    int                 `json:"version"`
	Exceptions []securityException `json:"exceptions"`
}

type securityException struct {
	Scanner   string `json:"scanner"`
	Package   string `json:"package,omitempty"`
	CVE       string `json:"cve,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Platform  string `json:"platform"`
	Reason    string `json:"reason"`
	Owner     string `json:"owner"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now().UTC()))
}

func run(args []string, stdout, stderr io.Writer, now time.Time) int {
	path := defaultRegistryPath
	queryScanner := ""
	queryPlatform := ""
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] != "--active-identifiers":
		path = args[0]
	case len(args) == 4 && args[0] == "--active-identifiers":
		queryScanner = args[1]
		queryPlatform = args[2]
		path = args[3]
		if !isAllowedScanner(queryScanner) || !isAllowedPlatform(queryPlatform) {
			fmt.Fprintln(stderr, "active identifier query uses an unsupported scanner or platform")
			return 2
		}
	default:
		fmt.Fprintln(stderr, "usage: validate-security-exceptions [registry-path]")
		fmt.Fprintln(stderr, "       validate-security-exceptions --active-identifiers <scanner> <platform> <registry-path>")
		return 2
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, "security exception registry validation failed: could not read registry")
		return 1
	}

	registry, err := parseRegistry(data)
	if err == nil {
		err = validateRegistry(registry, now.UTC())
	}
	if err != nil {
		fmt.Fprintf(stderr, "security exception registry validation failed: %s\n", err)
		return 1
	}

	if queryScanner != "" {
		for _, identifier := range activeIdentifiers(registry, queryScanner, queryPlatform) {
			fmt.Fprintln(stdout, identifier)
		}
	}
	return 0
}

func activeIdentifiers(registry exceptionRegistry, scanner, platform string) []string {
	identifiers := make([]string, 0)
	for _, exception := range registry.Exceptions {
		if exception.Scanner != scanner || exception.Platform != platform {
			continue
		}
		for _, identifier := range []struct {
			kind  string
			value string
		}{
			{kind: "package", value: exception.Package},
			{kind: "cve", value: exception.CVE},
			{kind: "rule", value: exception.Rule},
		} {
			if identifier.value != "" {
				identifiers = append(identifiers, identifier.kind+":"+identifier.value)
			}
		}
	}
	sort.Strings(identifiers)
	unique := identifiers[:0]
	for _, identifier := range identifiers {
		if len(unique) == 0 || unique[len(unique)-1] != identifier {
			unique = append(unique, identifier)
		}
	}
	return unique
}

func parseRegistry(data []byte) (exceptionRegistry, error) {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return exceptionRegistry{}, errors.New("registry is not strict JSON")
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return exceptionRegistry{}, errors.New("registry is not a JSON object")
	}
	if !hasOnlyKeys(root, "version", "exceptions") {
		return exceptionRegistry{}, errors.New("registry contains an unknown field")
	}
	versionJSON, hasVersion := root["version"]
	exceptionsJSON, hasExceptions := root["exceptions"]
	if !hasVersion || !hasExceptions {
		return exceptionRegistry{}, errors.New("registry must contain version and exceptions")
	}
	if isJSONNull(versionJSON) || isJSONNull(exceptionsJSON) {
		return exceptionRegistry{}, errors.New("registry fields must not be null")
	}

	var registry exceptionRegistry
	if err := json.Unmarshal(versionJSON, &registry.Version); err != nil {
		return exceptionRegistry{}, errors.New("registry version has an invalid type")
	}
	var rawExceptions []json.RawMessage
	if err := json.Unmarshal(exceptionsJSON, &rawExceptions); err != nil {
		return exceptionRegistry{}, errors.New("registry exceptions must be an array")
	}
	registry.Exceptions = make([]securityException, 0, len(rawExceptions))
	for index, raw := range rawExceptions {
		exception, err := parseException(raw, index)
		if err != nil {
			return exceptionRegistry{}, err
		}
		registry.Exceptions = append(registry.Exceptions, exception)
	}
	return registry, nil
}

func parseException(raw json.RawMessage, index int) (securityException, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return securityException{}, fmt.Errorf("exceptions[%d] must be an object", index)
	}
	if !hasOnlyKeys(fields, "scanner", "package", "cve", "rule", "platform", "reason", "owner", "created_at", "expires_at") {
		return securityException{}, fmt.Errorf("exceptions[%d] contains an unknown field", index)
	}
	for _, field := range []string{"scanner", "package", "cve", "rule", "platform", "reason", "owner", "created_at", "expires_at"} {
		if value, ok := fields[field]; ok && isJSONNull(value) {
			return securityException{}, fmt.Errorf("exceptions[%d].%s must not be null", index, field)
		}
	}

	var exception securityException
	if err := json.Unmarshal(raw, &exception); err != nil {
		return securityException{}, fmt.Errorf("exceptions[%d] contains a value with an invalid type", index)
	}
	return exception, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate object key")
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func hasOnlyKeys(values map[string]json.RawMessage, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range values {
		if _, ok := known[key]; !ok {
			return false
		}
	}
	return true
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func validateRegistry(registry exceptionRegistry, now time.Time) error {
	if registry.Version != 1 {
		return errors.New("registry version must be 1")
	}
	seen := make(map[string]struct{}, len(registry.Exceptions))
	for index, exception := range registry.Exceptions {
		if err := validateException(exception, index, now, seen); err != nil {
			return err
		}
	}
	return nil
}

func validateException(exception securityException, index int, now time.Time, seen map[string]struct{}) error {
	requiredValues := []struct {
		field string
		value string
	}{
		{field: "scanner", value: exception.Scanner},
		{field: "platform", value: exception.Platform},
		{field: "reason", value: exception.Reason},
		{field: "owner", value: exception.Owner},
		{field: "created_at", value: exception.CreatedAt},
		{field: "expires_at", value: exception.ExpiresAt},
	}
	for _, required := range requiredValues {
		if !isCleanRequiredValue(required.value) {
			return fmt.Errorf("exceptions[%d].%s must be non-empty without surrounding whitespace", index, required.field)
		}
	}

	if !isAllowedScanner(exception.Scanner) {
		return fmt.Errorf("exceptions[%d].scanner is not supported", index)
	}
	if !isAllowedPlatform(exception.Platform) {
		return fmt.Errorf("exceptions[%d].platform is not supported", index)
	}
	if len(exception.Reason) > 500 {
		return fmt.Errorf("exceptions[%d].reason is too long", index)
	}
	if len(exception.Owner) > 100 || !ownerPattern.MatchString(exception.Owner) {
		return fmt.Errorf("exceptions[%d].owner is invalid", index)
	}
	if exception.Reason == "*" || exception.Reason == "?" {
		return fmt.Errorf("exceptions[%d].reason must not be a wildcard", index)
	}

	identityValues := []struct {
		field string
		value string
	}{
		{field: "package", value: exception.Package},
		{field: "cve", value: exception.CVE},
		{field: "rule", value: exception.Rule},
	}
	identityCount := 0
	for _, identity := range identityValues {
		if identity.value == "" {
			continue
		}
		identityCount++
		if !isCleanRequiredValue(identity.value) || !identityTokenPattern.MatchString(identity.value) || containsWildcard(identity.value) || len(identity.value) > 300 {
			return fmt.Errorf("exceptions[%d].%s is invalid", index, identity.field)
		}
	}
	if identityCount != 1 {
		return fmt.Errorf("exceptions[%d] must contain exactly one of package, cve, or rule", index)
	}
	if exception.CVE != "" && !cvePattern.MatchString(exception.CVE) {
		return fmt.Errorf("exceptions[%d].cve is invalid", index)
	}
	if !scannerAcceptsIdentity(exception) {
		return fmt.Errorf("exceptions[%d] uses an unsupported identity type for its scanner", index)
	}

	createdAt, err := time.Parse(time.RFC3339, exception.CreatedAt)
	if err != nil {
		return fmt.Errorf("exceptions[%d].created_at must be RFC3339", index)
	}
	expiresAt, err := time.Parse(time.RFC3339, exception.ExpiresAt)
	if err != nil {
		return fmt.Errorf("exceptions[%d].expires_at must be RFC3339", index)
	}
	if createdAt.After(now) {
		return fmt.Errorf("exceptions[%d].created_at must not be in the future", index)
	}
	if !expiresAt.After(now) {
		return fmt.Errorf("exceptions[%d].expires_at has expired", index)
	}
	if !expiresAt.After(createdAt) {
		return fmt.Errorf("exceptions[%d].expires_at must be after created_at", index)
	}
	if expiresAt.Sub(createdAt) > maxExceptionDuration {
		return fmt.Errorf("exceptions[%d] exceeds the 30-day validity limit", index)
	}

	identity := strings.Join([]string{exception.Scanner, exception.Package, exception.CVE, exception.Rule, exception.Platform}, "\x00")
	if _, duplicate := seen[identity]; duplicate {
		return fmt.Errorf("exceptions[%d] duplicates an existing finding identity", index)
	}
	seen[identity] = struct{}{}
	return nil
}

func scannerAcceptsIdentity(exception securityException) bool {
	switch exception.Scanner {
	case "codeql":
		return exception.Rule != ""
	case "govulncheck":
		return exception.Rule != "" || exception.CVE != ""
	case "bun-audit":
		return exception.CVE != ""
	case "trivy":
		return exception.Package != "" || exception.CVE != ""
	default:
		return false
	}
}

func isCleanRequiredValue(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func containsWildcard(value string) bool {
	return strings.ContainsAny(value, "*?[]{}")
}

func isAllowedScanner(scanner string) bool {
	switch scanner {
	case "codeql", "govulncheck", "bun-audit", "trivy":
		return true
	default:
		return false
	}
}

func isAllowedPlatform(platform string) bool {
	switch platform {
	case "source", "linux/amd64", "linux/arm64":
		return true
	default:
		return false
	}
}
