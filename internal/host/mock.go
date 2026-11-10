package host

import (
	"context"
	"io"
	"sync"
)

// CommandCall records a call made to MockCommandRunner.
type CommandCall struct {
	Method  string
	Command string
	Script  string
	Args    []string
}

// MockCommandRunner is a thread-safe, configurable mock implementation of CommandRunner.
type MockCommandRunner struct {
	mu sync.Mutex

	RunFn                    func(command string) (string, error)
	RunContextFn             func(ctx context.Context, command string) (string, error)
	RunStreamFn              func(command string, stdout, stderr io.Writer) error
	RunStreamContextFn       func(ctx context.Context, command string, stdout, stderr io.Writer) error
	RunScriptFn              func(script string, args ...string) (string, error)
	RunScriptContextFn       func(ctx context.Context, script string, args ...string) (string, error)
	RunScriptStreamFn        func(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error
	RunScriptStreamContextFn func(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error

	Calls []CommandCall
}

var _ CommandRunner = (*MockCommandRunner)(nil)

func (m *MockCommandRunner) record(call CommandCall) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, call)
}

// RecordedCalls returns a snapshot copy of all recorded calls.
func (m *MockCommandRunner) RecordedCalls() []CommandCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]CommandCall, len(m.Calls))
	copy(copied, m.Calls)
	return copied
}

func (m *MockCommandRunner) Run(command string) (string, error) {
	m.record(CommandCall{Method: "Run", Command: command})
	if m.RunFn != nil {
		return m.RunFn(command)
	}
	return "", nil
}

func (m *MockCommandRunner) RunContext(ctx context.Context, command string) (string, error) {
	m.record(CommandCall{Method: "RunContext", Command: command})
	if m.RunContextFn != nil {
		return m.RunContextFn(ctx, command)
	}
	if m.RunFn != nil {
		return m.RunFn(command)
	}
	return "", nil
}

func (m *MockCommandRunner) RunStream(command string, stdout, stderr io.Writer) error {
	m.record(CommandCall{Method: "RunStream", Command: command})
	if m.RunStreamFn != nil {
		return m.RunStreamFn(command, stdout, stderr)
	}
	return nil
}

func (m *MockCommandRunner) RunStreamContext(ctx context.Context, command string, stdout, stderr io.Writer) error {
	m.record(CommandCall{Method: "RunStreamContext", Command: command})
	if m.RunStreamContextFn != nil {
		return m.RunStreamContextFn(ctx, command, stdout, stderr)
	}
	if m.RunStreamFn != nil {
		return m.RunStreamFn(command, stdout, stderr)
	}
	return nil
}

func (m *MockCommandRunner) RunScript(script string, args ...string) (string, error) {
	m.record(CommandCall{Method: "RunScript", Script: script, Args: args})
	if m.RunScriptFn != nil {
		return m.RunScriptFn(script, args...)
	}
	return "", nil
}

func (m *MockCommandRunner) RunScriptContext(ctx context.Context, script string, args ...string) (string, error) {
	m.record(CommandCall{Method: "RunScriptContext", Script: script, Args: args})
	if m.RunScriptContextFn != nil {
		return m.RunScriptContextFn(ctx, script, args...)
	}
	if m.RunScriptFn != nil {
		return m.RunScriptFn(script, args...)
	}
	return "", nil
}

func (m *MockCommandRunner) RunScriptStream(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
	m.record(CommandCall{Method: "RunScriptStream", Script: script, Args: args})
	if m.RunScriptStreamFn != nil {
		return m.RunScriptStreamFn(ctx, script, stdout, stderr, args...)
	}
	return nil
}

func (m *MockCommandRunner) RunScriptStreamContext(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error {
	m.record(CommandCall{Method: "RunScriptStreamContext", Script: script, Args: args})
	if m.RunScriptStreamContextFn != nil {
		return m.RunScriptStreamContextFn(ctx, script, stdout, stderr, args...)
	}
	if m.RunScriptStreamFn != nil {
		return m.RunScriptStreamFn(ctx, script, stdout, stderr, args...)
	}
	return nil
}
