package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// SandboxConfig holds isolation options for running commands in bubblewrap.
type SandboxConfig struct {
	UnshareAll bool
	RoBinds    []string
	RwBinds    []string
	Binds      []string
	Tmpfs      []string
	WorkDir    string
	Env        map[string]string
	Stdout     io.Writer
	Stderr     io.Writer
	Stdin      io.Reader
}

// BuildArgs converts SandboxConfig and target command into bubblewrap CLI arguments.
func (cfg SandboxConfig) BuildArgs(command string, args ...string) []string {
	var bwrapArgs []string

	if cfg.UnshareAll {
		bwrapArgs = append(bwrapArgs, "--unshare-all")
	}
	bwrapArgs = append(bwrapArgs, "--die-with-parent", "--new-session", "--clearenv")

	for _, ro := range cfg.RoBinds {
		bwrapArgs = append(bwrapArgs, "--ro-bind", ro, ro)
	}
	for _, rw := range cfg.RwBinds {
		bwrapArgs = append(bwrapArgs, "--bind", rw, rw)
	}
	for _, b := range cfg.Binds {
		bwrapArgs = append(bwrapArgs, "--bind", b, b)
	}
	for _, t := range cfg.Tmpfs {
		bwrapArgs = append(bwrapArgs, "--tmpfs", t)
	}

	if cfg.WorkDir != "" {
		bwrapArgs = append(bwrapArgs, "--chdir", cfg.WorkDir)
	}

	for k, v := range cfg.Env {
		bwrapArgs = append(bwrapArgs, "--setenv", k, v)
	}

	bwrapArgs = append(bwrapArgs, command)
	bwrapArgs = append(bwrapArgs, args...)

	return bwrapArgs
}

// Run executes a command inside bubblewrap (bwrap).
func Run(ctx context.Context, cfg SandboxConfig, command string, args ...string) error {
	bwrapPath, err := LookBwrap()
	if err != nil {
		return err
	}

	fullArgs := cfg.BuildArgs(command, args...)
	cmd := exec.CommandContext(ctx, bwrapPath, fullArgs...)
	cmd.Stdout = cfg.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = cfg.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	cmd.Stdin = cfg.Stdin

	return cmd.Run()
}

// LookBwrap verifies if bubblewrap binary is installed on the host.
func LookBwrap() (string, error) {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return "", fmt.Errorf("bubblewrap (bwrap) not found in PATH: %w", err)
	}
	return path, nil
}
