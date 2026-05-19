package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const AppName = "tickle"

func ConfigHome() (string, error) {
	if v := os.Getenv("TICKLE_CONFIG_HOME"); v != "" {
		return filepath.Clean(v), nil
	}
	if runtime.GOOS == "linux" {
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return filepath.Join(v, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", AppName), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(base, "Tickle"), nil
	}
	return filepath.Join(base, AppName), nil
}

func DataHome() (string, error) {
	if v := os.Getenv("TICKLE_DATA_HOME"); v != "" {
		return filepath.Clean(v), nil
	}
	switch runtime.GOOS {
	case "linux":
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", AppName), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", AppName), nil
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "Tickle"), nil
		}
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "Tickle"), nil
	default:
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, AppName), nil
	}
}

func JobsDir() (string, error) {
	base, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "jobs"), nil
}

func ScriptsDir() (string, error) {
	base, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "scripts"), nil
}

func TemplatesDir() (string, error) {
	base, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "templates"), nil
}

func StateDir() (string, error) {
	base, err := DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "state"), nil
}

func RunsDir() (string, error) {
	base, err := DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "runs"), nil
}

func LogsDir() (string, error) {
	base, err := DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "logs"), nil
}

func BinDir() (string, error) {
	base, err := DataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bin"), nil
}

func StableBinaryPath() (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}
	name := AppName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name), nil
}

func EnsureBaseDirs() error {
	dirs := make([]string, 0, 7)
	for _, fn := range []func() (string, error){JobsDir, ScriptsDir, TemplatesDir, StateDir, RunsDir, LogsDir, BinDir} {
		dir, err := fn()
		if err != nil {
			return err
		}
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func HasPathToken(value string) bool {
	return value == "@config" ||
		value == "@data" ||
		strings.HasPrefix(value, "@config/") ||
		strings.HasPrefix(value, "@data/") ||
		strings.HasPrefix(value, `@config\`) ||
		strings.HasPrefix(value, `@data\`)
}

func ExpandPathToken(value string) (string, error) {
	switch {
	case value == "@config":
		return ConfigHome()
	case value == "@data":
		return DataHome()
	case strings.HasPrefix(value, "@config/"):
		base, err := ConfigHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, filepath.FromSlash(strings.TrimPrefix(value, "@config/"))), nil
	case strings.HasPrefix(value, "@data/"):
		base, err := DataHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, filepath.FromSlash(strings.TrimPrefix(value, "@data/"))), nil
	case strings.HasPrefix(value, `@config\`):
		base, err := ConfigHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, strings.TrimPrefix(value, `@config\`)), nil
	case strings.HasPrefix(value, `@data\`):
		base, err := DataHome()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, strings.TrimPrefix(value, `@data\`)), nil
	default:
		return value, nil
	}
}

func ExistingFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
