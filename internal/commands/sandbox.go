package commands

type SandboxMode int

const (
	SandboxAutoAllow SandboxMode = iota
	SandboxRegular
	SandboxOff
)

func SandboxModeLabels() []string {
	return []string{
		"Sandbox on + auto-allow (recommended)",
		"Sandbox on + regular permissions",
		"Sandbox off",
	}
}

func SandboxModeDescriptions() []string {
	return []string{
		"Commands run inside the sandbox automatically without confirmation. Explicit deny rules still apply.",
		"Commands run inside the sandbox but still require permission confirmation.",
		"No OS-level isolation; relies on application-layer permissions only.",
	}
}
