package buildinfo

const appName = "agentreceipt"

var version = "dev"

// Name returns the binary name used by command help, smoke tests, and artifacts.
func Name() string {
	return appName
}

// Version returns the build version injected at release time.
func Version() string {
	if version == "" {
		return "dev"
	}

	return version
}
