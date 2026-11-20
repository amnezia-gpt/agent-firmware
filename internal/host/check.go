package host

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// WorkloadPlatform is the Linux container platform native to this firmware's host.
func WorkloadPlatform() ocispec.Platform {
	return ocispec.Platform{OS: "linux", Architecture: runtime.GOARCH}
}

// InstallCheck reports whether the host meets the firmware's install requirements.
func InstallCheck() string {
	if reason := AccessCheck(); reason != "" {
		return reason
	}
	if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		version, err := CommandOutput(ctx, 128, "/usr/bin/sw_vers", "-productVersion")
		major, _, _ := strings.Cut(strings.TrimSpace(string(version)), ".")
		number, parseErr := strconv.Atoi(major)
		if err != nil || parseErr != nil || number < 15 {
			return "unsupported_macos_version"
		}
		return ""
	}
	if os.Getegid() != 0 {
		return "root_required"
	}
	return ""
}

// AccessCheck applies the lifecycle command's platform and privilege rules.
func AccessCheck() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if os.Getuid() == 0 || os.Geteuid() == 0 {
			return "root_forbidden"
		}
		if DefaultLayout().State == "" {
			return "invalid_home_directory"
		}
		return ""
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return "unsupported_host"
	}
	if os.Geteuid() != 0 {
		return "root_required"
	}
	return ""
}
