package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfigPath returns config.yaml next to the executable.
func DefaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.yaml"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "config.yaml")
}

// StateFile returns <config-name>.state.yaml next to the loaded config file.
// e.g. /opt/foo/config.yaml → /opt/foo/config.state.yaml
func (c *Config) StateFile() string {
	return sidecarPath(c.configPath, ".state.yaml")
}

// LockFile returns <config-name>.lock next to the loaded config file.
func (c *Config) LockFile() string {
	return sidecarPath(c.configPath, ".lock")
}

// LogFile returns <config-name>.log next to the loaded config file.
func (c *Config) LogFile() string {
	return sidecarPath(c.configPath, ".log")
}

func sidecarPath(configPath, suffix string) string {
	dir := filepath.Dir(configPath)
	base := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	return filepath.Join(dir, base+suffix)
}
