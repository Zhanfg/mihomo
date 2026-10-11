package cfir

import "fmt"

func PlatformFromGOOS(goos string) (Platform, error) {
	switch goos {
	case "android":
		return PlatformAndroid, nil
	case "linux":
		return PlatformLinux, nil
	case "windows":
		return PlatformWindows, nil
	case "darwin":
		return PlatformMacOS, nil
	case "ios":
		return PlatformIOS, nil
	default:
		return PlatformInvalid, fmt.Errorf("cfir: unsupported GOOS %q", goos)
	}
}
