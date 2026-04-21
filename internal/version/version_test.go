package version

import "testing"

func TestCLIMessage(t *testing.T) {
	got := CLIMessage("stagehand", "implementation starts in later stories")
	want := "stagehand 0.1.0-alpha.0: implementation starts in later stories"

	if got != want {
		t.Fatalf("CLIMessage() = %q, want %q", got, want)
	}
}

func TestArtifactAndCLIVersionMatch(t *testing.T) {
	if ArtifactVersion != CLIVersion {
		t.Fatalf("ArtifactVersion = %q, CLIVersion = %q; expected them to match", ArtifactVersion, CLIVersion)
	}
}
