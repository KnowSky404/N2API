package buildinfo

import "strings"

const (
	developmentVersion   = "dev"
	unknownCommit        = "unknown"
	developmentBuildTime = "1970-01-01T00:00:00Z"
)

// These values are replaced in release images with Go linker -X flags.
var (
	Version   = developmentVersion
	Commit    = unknownCommit
	BuildTime = developmentBuildTime
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
}

func Current() Info {
	return normalize(Version, Commit, BuildTime)
}

func normalize(version, commit, buildTime string) Info {
	return Info{
		Version: valueOrDefault(version, developmentVersion),
		Commit:  valueOrDefault(commit, unknownCommit),
		BuiltAt: valueOrDefault(buildTime, developmentBuildTime),
	}
}

func valueOrDefault(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}
