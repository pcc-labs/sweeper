package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// mixtapeFor returns the stereOS mixtape published for the host architecture.
// The registry splits the coder mixtape by arch (`mb mixtapes list`), so there
// is no single portable name. The previous hardcoded "opencode-mixtape:latest"
// is no longer served by the registry — it only resolved on machines that had
// already pulled it, so --vm worked locally and failed everywhere else.
func mixtapeFor(goarch string) string {
	if goarch == "amd64" {
		return "coder-x86:latest"
	}
	return "coder-arm64:latest"
}

const jcardTemplate = `mixtape = "%s"
name = "%s"

[resources]
cpus = 4
memory = "8GiB"

[[shared]]
host = "%s"
guest = "/workspace"
readonly = false

[secrets]
ANTHROPIC_API_KEY = "%s"
`

// GenerateJcard writes an ephemeral jcard.toml to dir and returns its path.
// The ANTHROPIC_API_KEY is read from the environment and embedded in the jcard
// so the VM has access to the API key without host env leakage.
func GenerateJcard(dir, name, hostProjectDir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating jcard dir: %w", err)
	}
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	path := filepath.Join(dir, "jcard.toml")
	content := fmt.Sprintf(jcardTemplate, mixtapeFor(runtime.GOARCH), name, hostProjectDir, apiKey)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("writing jcard: %w", err)
	}
	return path, nil
}

// CleanupJcard removes the jcard file. Safe to call on nonexistent paths.
func CleanupJcard(path string) {
	_ = os.Remove(path)
}
