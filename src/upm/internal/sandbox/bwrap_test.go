package sandbox

import (
	"strings"
	"testing"
)

func TestSandboxConfig_BuildArgs(t *testing.T) {
	cfg := SandboxConfig{
		UnshareAll: true,
		RoBinds:    []string{"/usr", "/lib"},
		RwBinds:    []string{"/destdir", "/tmp"},
		Tmpfs:      []string{"/var"},
		WorkDir:    "/tmp",
		Env: map[string]string{
			"DESTDIR": "/destdir",
			"PATH":    "/usr/bin:/bin",
		},
	}

	args := cfg.BuildArgs("/bin/sh", "build.sh")

	expectedFlags := []string{
		"--unshare-all",
		"--die-with-parent",
		"--new-session",
		"--clearenv",
		"--ro-bind", "/usr", "/usr",
		"--bind", "/destdir", "/destdir",
		"--tmpfs", "/var",
		"--chdir", "/tmp",
		"--setenv", "DESTDIR", "/destdir",
		"/bin/sh",
		"build.sh",
	}

	argStr := strings.Join(args, " ")
	for _, flag := range expectedFlags {
		if !strings.Contains(argStr, flag) {
			t.Errorf("expected bwrap args to contain %q, but got:\n%s", flag, argStr)
		}
	}
}
