package host

import (
	"context"
	"io"
)

// CommandRunner defines the execution seam for remote host commands and scripts.
type CommandRunner interface {
	Run(command string) (string, error)
	RunContext(ctx context.Context, command string) (string, error)
	RunStream(command string, stdout, stderr io.Writer) error
	RunStreamContext(ctx context.Context, command string, stdout, stderr io.Writer) error
	RunScript(script string, args ...string) (string, error)
	RunScriptContext(ctx context.Context, script string, args ...string) (string, error)
	RunScriptStream(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error
	RunScriptStreamContext(ctx context.Context, script string, stdout, stderr io.Writer, args ...string) error
}
