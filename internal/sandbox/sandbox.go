package sandbox

type Sandbox interface {
	Wrap(command string, config Config) (string, error)
	Available() bool
}

type Config struct {
	AllowWrite     []string
	DenyWrite      []string
	NetworkEnabled bool
}

func New() Sandbox {
	return newPlatformSandbox()
}
