package buildinfo

import "testing"

func TestNormalizeUsesExplicitDevelopmentDefaults(t *testing.T) {
	got := normalize(" ", "", "\t")
	want := Info{
		Version: developmentVersion,
		Commit:  unknownCommit,
		BuiltAt: developmentBuildTime,
	}
	if got != want {
		t.Fatalf("normalize() = %+v, want %+v", got, want)
	}
}

func TestNormalizePreservesInjectedBuildIdentity(t *testing.T) {
	got := normalize(
		" sha-0123456789ab ",
		" 0123456789abcdef0123456789abcdef01234567 ",
		" 2026-07-21T08:30:00Z ",
	)
	want := Info{
		Version: "sha-0123456789ab",
		Commit:  "0123456789abcdef0123456789abcdef01234567",
		BuiltAt: "2026-07-21T08:30:00Z",
	}
	if got != want {
		t.Fatalf("normalize() = %+v, want %+v", got, want)
	}
}

func TestCurrentReadsInjectedVariables(t *testing.T) {
	previousVersion, previousCommit, previousBuildTime := Version, Commit, BuildTime
	t.Cleanup(func() {
		Version, Commit, BuildTime = previousVersion, previousCommit, previousBuildTime
	})

	Version = "sha-0123456789ab"
	Commit = "0123456789abcdef0123456789abcdef01234567"
	BuildTime = "2026-07-21T08:30:00Z"

	want := Info{Version: Version, Commit: Commit, BuiltAt: BuildTime}
	if got := Current(); got != want {
		t.Fatalf("Current() = %+v, want %+v", got, want)
	}
}
