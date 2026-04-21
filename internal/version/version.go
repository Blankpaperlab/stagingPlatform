package version

import "fmt"

const (
	ArtifactVersion = "0.1.0-alpha.0"
	CLIVersion      = ArtifactVersion
)

func CLIMessage(binary, note string) string {
	return fmt.Sprintf("%s %s: %s", binary, CLIVersion, note)
}
