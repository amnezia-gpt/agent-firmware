package host

import (
	"os"
	"runtime"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// WorkloadPlatform is the Linux container platform native to this firmware's host.
func WorkloadPlatform() ocispec.Platform {
	return ocispec.Platform{OS: "linux", Architecture: runtime.GOARCH}
}

// InstallCheck reports whether the host meets the firmware's install requirements.
func InstallCheck() string {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return "unsupported_host"
	}
	if os.Geteuid() != 0 || os.Getegid() != 0 {
		return "root_required"
	}
	return ""
}
