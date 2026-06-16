package buildinfo

const appName = "agentreceipt"

// Name returns the binary name used by command help, smoke tests, and artifacts.
func Name() string {
	return appName
}
