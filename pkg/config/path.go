package config

// DefaultPath returns the config file path used when --config is not given.
//
// The service path comes first (decision 12): the agent is always a system
// service, and the per-user paths exist only for running `agent run` in the
// foreground as a normal user. Installers and unit files that need a fixed
// location pass --config explicitly, which overrides all of this.
func DefaultPath() string {
	if runningAsSystem() {
		return servicePath()
	}
	return userPath()
}

// configDirName is the directory name under the per-user config dir. Linux
// keeps the lowercase package name; macOS and Windows use the product name.
func configDirName() string {
	if goos == "linux" {
		return "thai-smartcard"
	}
	return "ThaiSmartcard"
}
