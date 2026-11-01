package storage

import "os"

const (
	YukinoDirName    = ".yukino"
	SessionsDirName  = "sessions"
	ArtifactsDirName = "artifacts"
	ProjectsDirName  = "projects"

	InvalidSessionIDError = "Invalid session ID"

	PrivateDirMode           os.FileMode = 0o700
	PrivateFileMode          os.FileMode = 0o600
	PrivateStateNotFileError             = "Private state must be a regular file"
)

var PublicStateSegments = map[string]bool{
	"worktrees": true,
	"skills":    true,
	"agents":    true,
	"prompts":   true,
	"AGENTS.md": true,
}
