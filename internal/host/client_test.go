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
	"testing"
	"time"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintf(os.Stderr, "no command\n")
		os.Exit(2)
	}

	cmd := args[0]
	switch cmd {
	case "ssh":
		if os.Getenv("TEST_SSH_FAIL") == "1" {
			_, _ = fmt.Fprintf(os.Stderr, "simulated ssh failure: connection refused\n")
			os.Exit(1)
		}
		if os.Getenv("TEST_SSH_HANG") == "1" {
			time.Sleep(5 * time.Second)
			return
		}
		if os.Getenv("TEST_PRINT_REMOTE_CMD") == "1" {
			_, _ = fmt.Fprintf(os.Stdout, "REMOTE_CMD:%s\n", args[len(args)-1])
			return
		}
		if os.Getenv("TEST_ECHO_STDIN") == "1" {
			stdinBytes, _ := io.ReadAll(os.Stdin)
			if strings.Contains(string(stdinBytes), "fail_me") {
				_, _ = fmt.Fprintf(os.Stderr, "script failed: assertion error\n")
				os.Exit(1)
			}
			_, _ = fmt.Fprintf(os.Stdout, "%s\n", strings.TrimSpace(string(stdinBytes)))
			return
		}
		_, _ = fmt.Fprintf(os.Stdout, "command output: success\nstreaming line 1\nstreaming line 2\n")
		_, _ = fmt.Fprintf(os.Stderr, "stderr message\n")
	default:
		_, _ = fmt.Fprintf(os.Stderr, "unknown command %s\n", cmd)
		os.Exit(2)
	}
}

func fakeExecCommand(env ...string) func(string, ...string) *exec.Cmd {
	return func(command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		cmd.Env = append(cmd.Env, env...)
		return cmd
	}
}

func fakeExecCommandContext(env ...string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, command string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", command}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		cmd.Env = append(cmd.Env, env...)
		return cmd
	}
}

func TestRunContext_Success(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext()

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx := context.Background()

	out, err := client.RunContext(ctx, "uptime")
	if err != nil {
		t.Fatalf("expected RunContext to succeed, got error: %v", err)
	}
	if !strings.Contains(out, "command output: success") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunContext_Failure(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_FAIL=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx := context.Background()

	out, err := client.RunContext(ctx, "uptime")
	if err == nil {
		t.Fatal("expected RunContext to return an error, got nil")
	}
	if out != "" {
		t.Errorf("expected empty stdout on error, got: %q", out)
	}
	if !strings.Contains(err.Error(), "192.168.1.10") {
		t.Errorf("expected error to mention host, got: %v", err)
	}
	if !strings.Contains(err.Error(), "simulated ssh failure") {
		t.Errorf("expected error to include stderr message, got: %v", err)
	}
}

func TestRunContext_Timeout(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_HANG=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.RunContext(ctx, "hang_forever")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected RunContext to fail on timeout, got nil")
	}
	if duration >= 2*time.Second {
		t.Fatalf("RunContext did not terminate on timeout, took %v", duration)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("expected error to wrap context.DeadlineExceeded, got: %v", err)
	}
}

func TestRunContext_Cancellation(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_HANG=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.RunContext(ctx, "hang_forever")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected RunContext to fail on cancellation, got nil")
	}
	if duration >= 2*time.Second {
		t.Fatalf("RunContext did not terminate on cancellation, took %v", duration)
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected error to wrap context.Canceled, got: %v", err)
	}
}

func TestRun_BackwardCompatibility(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext()

	client := NewSSHClient("virtuser", "192.168.1.10")

	out, err := client.Run("echo compat")
	if err != nil {
		t.Fatalf("expected legacy Run to succeed, got: %v", err)
	}
	if !strings.Contains(out, "command output: success") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunScriptContext_Success(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_ECHO_STDIN=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx := context.Background()

	out, err := client.RunScriptContext(ctx, "hello script")
	if err != nil {
		t.Fatalf("expected RunScriptContext to succeed, got: %v", err)
	}
	if !strings.Contains(out, "hello script") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunScriptContext_Failure(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_ECHO_STDIN=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx := context.Background()

	_, err := client.RunScriptContext(ctx, "fail_me please")
	if err == nil {
		t.Fatal("expected RunScriptContext to fail, got nil")
	}
	if !strings.Contains(err.Error(), "script failed: assertion error") {
		t.Errorf("expected error to include script stderr, got: %v", err)
	}
}

func TestRunScriptContext_Timeout(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_HANG=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.RunScriptContext(ctx, "sleep 100")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected RunScriptContext to fail on timeout, got nil")
	}
	if duration >= 2*time.Second {
		t.Fatalf("RunScriptContext did not terminate on timeout, took %v", duration)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("expected error to wrap context.DeadlineExceeded, got: %v", err)
	}
}

func TestRunScriptContext_Cancellation(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_HANG=1")

	client := NewSSHClient("virtuser", "192.168.1.10")
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.RunScriptContext(ctx, "sleep 100")
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected RunScriptContext to fail on cancellation, got nil")
	}
	if duration >= 2*time.Second {
		t.Fatalf("RunScriptContext did not terminate on cancellation, took %v", duration)
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected error to wrap context.Canceled, got: %v", err)
	}
}

func TestRunScript_BackwardCompat(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_ECHO_STDIN=1")

	client := NewSSHClient("virtuser", "192.168.1.10")

	out, err := client.RunScript("hello script compat")
	if err != nil {
		t.Fatalf("expected legacy RunScript to succeed, got: %v", err)
	}
	if !strings.Contains(out, "hello script compat") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestRunStreamContext_Success(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext()

	client := NewSSHClient("testuser", "testhost")
	var stdout, stderr bytes.Buffer

	err := client.RunStreamContext(context.Background(), "uptime", &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected RunStreamContext to succeed, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "command output: success") {
		t.Errorf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunStream_BackwardCompat(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext()

	client := NewSSHClient("testuser", "testhost")
	var stdout, stderr bytes.Buffer

	err := client.RunStream("uptime", &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected RunStream to succeed, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "command output: success") {
		t.Errorf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunScriptStreamContext_Alias(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext()

	client := NewSSHClient("testuser", "testhost")
	var stdout, stderr bytes.Buffer

	err := client.RunScriptStreamContext(context.Background(), "echo test", &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected RunScriptStreamContext to succeed, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "streaming line 1") {
		t.Errorf("unexpected stdout: %q", stdout.String())
	}
}

func TestSSHOptions_DefaultArguments(t *testing.T) {
	client := NewSSHClient("virtuser", "192.168.1.10")
	cmd := client.newCommand(context.Background(), "echo test")

	expectedArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	}

	for _, expected := range expectedArgs {
		found := false
		for _, a := range cmd.Args {
			if a == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cmd.Args %v missing expected SSH argument %q", cmd.Args, expected)
		}
	}
}

func TestSSHClient_SubprocessLifecycleConfig(t *testing.T) {
	client := NewSSHClient("virtuser", "192.168.1.10")
	cmd := client.newCommand(context.Background(), "echo test")

	if cmd.Cancel == nil {
		t.Fatal("expected cmd.Cancel to be configured")
	}
	if cmd.WaitDelay != 2*time.Second {
		t.Errorf("expected cmd.WaitDelay = 2s, got %v", cmd.WaitDelay)
	}
}

func TestSSHClient_ErrorWrappingClassification(t *testing.T) {
	client := NewSSHClient("virtuser", "192.168.1.10")

	// 1. Timeout error wrapping
	timeoutCtx, cancelTimeout := context.WithTimeout(context.Background(), 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	cancelTimeout()

	errTimeout := client.wrapError(timeoutCtx, "probe", errors.New("signal: killed"), "timed out on remote")
	if !errors.Is(errTimeout, context.DeadlineExceeded) {
		t.Errorf("expected errTimeout to wrap context.DeadlineExceeded, got: %v", errTimeout)
	}
	if !strings.Contains(errTimeout.Error(), "192.168.1.10 timed out") {
		t.Errorf("expected host in timeout error: %v", errTimeout)
	}

	// 2. Cancellation error wrapping
	cancelCtx, cancelCancel := context.WithCancel(context.Background())
	cancelCancel()

	errCancel := client.wrapError(cancelCtx, "command", errors.New("signal: interrupt"), "canceled by signal")
	if !errors.Is(errCancel, context.Canceled) {
		t.Errorf("expected errCancel to wrap context.Canceled, got: %v", errCancel)
	}
	if !strings.Contains(errCancel.Error(), "192.168.1.10 canceled") {
		t.Errorf("expected host in canceled error: %v", errCancel)
	}

	// 3. Execution failure (non-context)
	errExec := client.wrapError(context.Background(), "command", errors.New("exit status 1"), "command not found")
	if errors.Is(errExec, context.DeadlineExceeded) || errors.Is(errExec, context.Canceled) {
		t.Errorf("execution error should not be classified as context error: %v", errExec)
	}
	if !strings.Contains(errExec.Error(), "failed: exit status 1 (stderr: command not found)") {
		t.Errorf("unexpected exec error message: %v", errExec)
	}
}

func TestRunScriptStream_Success(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext()

	client := NewSSHClient("testuser", "testhost")
	var stdout, stderr bytes.Buffer

	err := client.RunScriptStream(context.Background(), "echo test", &stdout, &stderr)
	if err != nil {
		t.Fatalf("expected RunScriptStream to succeed, got: %v", err)
	}

	if !strings.Contains(stdout.String(), "streaming line 1\nstreaming line 2") {
		t.Errorf("unexpected stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "stderr message") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunScriptStream_Failure(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_FAIL=1")

	client := NewSSHClient("testuser", "testhost")
	var stdout, stderr bytes.Buffer

	err := client.RunScriptStream(context.Background(), "echo test", &stdout, &stderr)
	if err == nil {
		t.Fatal("expected RunScriptStream to return an error, got nil")
	}
	if !strings.Contains(err.Error(), "testhost") {
		t.Errorf("expected error to mention host, got: %v", err)
	}
}

func TestRunScriptStream_ContextCancel(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_SSH_HANG=1")

	client := NewSSHClient("testuser", "testhost")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var stdout, stderr bytes.Buffer
	err := client.RunScriptStream(ctx, "echo test", &stdout, &stderr)
	if err == nil {
		t.Fatal("expected RunScriptStream to fail on context cancellation, got nil")
	}
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"foo'bar", "'foo'\\''bar'"},
		{"a'b'c", "'a'\\''b'\\''c'"},
		{"foo; rm -rf /", "'foo; rm -rf /'"},
		{"$HOME", "'$HOME'"},
		{"`whoami`", "'`whoami`'"},
		{"$(reboot)", "'$(reboot)'"},
		{"", "''"},
		{"'leading", "''\\''leading'"},
		{"trailing'", "'trailing'\\'''"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellEscape(tt.input)
			if got != tt.expected {
				t.Errorf("ShellEscape(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildBashCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{"no arguments", nil, "bash -s"},
		{"empty slice", []string{}, "bash -s"},
		{"single argument", []string{"my-net"}, "bash -s -- 'my-net'"},
		{"multiple arguments", []string{"arg1", "arg 2", "it's"}, "bash -s -- 'arg1' 'arg 2' 'it'\\''s'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBashCommand(tt.args)
			if got != tt.expected {
				t.Errorf("buildBashCommand(%v) = %q; want %q", tt.args, got, tt.expected)
			}
		})
	}
}

func TestRunScript_WithArguments(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_PRINT_REMOTE_CMD=1")

	origOld := execCommand
	defer func() { execCommand = origOld }()
	execCommand = fakeExecCommand("TEST_PRINT_REMOTE_CMD=1")

	client := NewSSHClient("testuser", "testhost")

	t.Run("no arguments", func(t *testing.T) {
		out, err := client.RunScript("script body")
		if err != nil {
			t.Fatalf("expected RunScript to succeed, got: %v", err)
		}
		if out != "REMOTE_CMD:bash -s" {
			t.Errorf("expected REMOTE_CMD:bash -s, got: %q", out)
		}
	})

	t.Run("with positional arguments", func(t *testing.T) {
		out, err := client.RunScript("script body", "first", "second arg", "single'quote")
		if err != nil {
			t.Fatalf("expected RunScript to succeed, got: %v", err)
		}
		expected := "REMOTE_CMD:bash -s -- 'first' 'second arg' 'single'\\''quote'"
		if out != expected {
			t.Errorf("expected %q, got: %q", expected, out)
		}
	})
}

func TestRunScriptStream_WithArguments(t *testing.T) {
	orig := execCommandContext
	defer func() { execCommandContext = orig }()
	execCommandContext = fakeExecCommandContext("TEST_PRINT_REMOTE_CMD=1")

	client := NewSSHClient("testuser", "testhost")
	var stdout, stderr bytes.Buffer
	err := client.RunScriptStream(context.Background(), "script body", &stdout, &stderr, "arg1", "arg2; id")
	if err != nil {
		t.Fatalf("expected RunScriptStream to succeed, got: %v", err)
	}

	expected := "REMOTE_CMD:bash -s -- 'arg1' 'arg2; id'"
	if !strings.Contains(stdout.String(), expected) {
		t.Errorf("expected %q in stdout, got: %q", expected, stdout.String())
	}
}
