package host

import (
	"os"
	"runtime"
)

// InstallCheck reports whether the host meets the firmware's install requirements.
func InstallCheck() string {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return "unsupported_host"
	}
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		return "root_required"
	}
	return ""
}
