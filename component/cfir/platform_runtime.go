package cfir

import "runtime"

func RuntimePlatform() (Platform, error) {
	return PlatformFromGOOS(runtime.GOOS)
}
