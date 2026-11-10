package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	execCommand        = exec.Command
	execCommandContext = exec.CommandContext
)

var defaultSSHArgs = []string{
	"-o", "BatchMode=yes",
	"-o", "StrictHostKeyChecking=accept-new",
	"-o", "ConnectTimeout=10",
	"-o", "ServerAliveInterval=15",
	"-o", "ServerAliveCountMax=3",
}

// ShellEscape wraps a string in single quotes and escapes any contained single quotes
// following POSIX shell quoting conventions: 'foo'\”bar'.
func ShellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildBashCommand constructs the remote SSH command string for bash -s, safely appending
// positional arguments when provided.
func buildBashCommand(args []string) string {
	if len(args) == 0 {
		return "bash -s"
	}
	escapedArgs := make([]string, len(args))
	for i, a := range args {
		escapedArgs[i] = ShellEscape(a)
	}
	return "bash -s -- " + strings.Join(escapedArgs, " ")
}

// SSHClient handles command execution on the remote Host hypervisor over SSH.
type SSHClient struct {
	Host string
	User string
}

var _ CommandRunner = (*SSHClient)(nil)

// NewSSHClient creates an SSH executor for user@host.
func NewSSHClient(user, host string) *SSHClient {
	return &SSHClient{
		Host: host,
		User: user,
	}
}

// Target returns the "user@host" connection string.
func (c *SSHClient) Target() string {
	return fmt.Sprintf("%s@%s", c.User, c.Host)
}

// newCommand constructs an *exec.Cmd configured with context, SSH options,
// graceful SIGINT cancellation, and a 2-second WaitDelay.
func (c *SSHClient) newCommand(ctx context.Context, remoteCmd string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	args := append([]string{}, defaultSSHArgs...)
	args = append(args, c.Target(), remoteCmd)
	cmd := execCommandContext(ctx, "ssh", args...)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Signal(os.Interrupt)
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// wrapError classifies the failure and wraps context errors (DeadlineExceeded, Canceled)
// using %w so callers can inspect them with errors.Is.
func (c *SSHClient) wrapError(ctx context.Context, op string, err error, stderr string) error {
	trimmedErr := strings.TrimSpace(stderr)
	if ctx != nil && ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if trimmedErr != "" {
				return fmt.Errorf("ssh %s on %s timed out: %w (stderr: %s)", op, c.Host, ctx.Err(), trimmedErr)
			}
			return fmt.Errorf("ssh %s on %s timed out: %w", op, c.Host, ctx.Err())
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			if trimmedErr != "" {
				return fmt.Errorf("ssh %s on %s canceled: %w (stderr: %s)", op, c.Host, ctx.Err(), trimmedErr)
			}
			return fmt.Errorf("ssh %s on %s canceled: %w", op, c.Host, ctx.Err())
		}
	}
	prefix := fmt.Sprintf("ssh %s", c.Host)
	if op == "script" {
		prefix = fmt.Sprintf("ssh script on %s", c.Host)
	} else if op == "streaming script" {
		prefix = fmt.Sprintf("ssh streaming script on %s", c.Host)
	} else if strings.HasPrefix(op, "streaming ") {
		prefix = fmt.Sprintf("ssh %s on %s", op, c.Host)
	}
	if trimmedErr != "" {
		return fmt.Errorf("%s failed: %w (stderr: %s)", prefix, err, trimmedErr)
	}
	return fmt.Errorf("%s failed: %w", prefix, err)
}

// RunContext executes a remote shell command string with context cancellation support.
func (c *SSHClient) RunContext(ctx context.Context, command string) (string, error) {
	cmd := c.newCommand(ctx, command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", c.wrapError(ctx, command, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Run executes a remote shell command string on the Host and returns combined stdout.
// Preserved for backward compatibility.
func (c *SSHClient) Run(command string) (string, error) {
	return c.RunContext(context.Background(), command)
}

// RunStreamContext executes a remote command string on the Host, streaming stdout and stderr
// in real time with context cancellation support.
func (c *SSHClient) RunStreamContext(ctx context.Context, command string, stdout, stderr io.Writer) error {
	cmd := c.newCommand(ctx, command)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return c.wrapError(ctx, fmt.Sprintf("streaming '%s'", command), err, "")
	}
	return nil
}

// RunStream executes a remote command string on the Host, streaming stdout and stderr in real time.
// Preserved for backward compatibility.
func (c *SSHClient) RunStream(command string, stdout, stderr io.Writer) error {
	return c.RunStreamContext(context.Background(), command, stdout, stderr)
}

// RunScriptContext pipes a multi-line script via stdin into bash -s on the remote Host with context.
func (c *SSHClient) RunScriptContext(ctx context.Context, script string, args ...string) (string, error) {
	remoteCmd := buildBashCommand(args)
	cmd := c.newCommand(ctx, remoteCmd)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", c.wrapError(ctx, "script", err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunScript pipes a multi-line script via stdin into bash -s on the remote Host.
// Preserved for backward compatibility.
func (c *SSHClient) RunScript(script string, args ...string) (string, error) {
	return c.RunScriptContext(context.Background(), script, args...)
}

// RunScriptStream pipes a multi-line script via stdin into bash -s on the remote Host,
// streaming stdout and stderr to the provided io.Writer destinations in real time.
func (c *SSHClient) RunScriptStream(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
	remoteCmd := buildBashCommand(args)
	cmd := c.newCommand(ctx, remoteCmd)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return c.wrapError(ctx, "streaming script", err, "")
	}
	return nil
}

// RunScriptStreamContext is a naming alias for RunScriptStream for full API symmetry.
func (c *SSHClient) RunScriptStreamContext(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
	return c.RunScriptStream(ctx, script, stdout, stderr, args...)
}
