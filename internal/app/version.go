package app

import "os"

// version is stamped by the release build with -ldflags "-X".
var version string

// useBuildVersion lets a bare release binary report its version, since GoFr
// and the settings page read it from APP_VERSION. An APP_VERSION set in the
// environment, as the container image does, still wins.
func useBuildVersion() {
	if version != "" && os.Getenv("APP_VERSION") == "" {
		_ = os.Setenv("APP_VERSION", version)
	}
}
