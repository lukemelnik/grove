package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// isTerminal reports whether the given file descriptor is a terminal.
// It is a variable so tests can override it.
var isTerminal = func(fd int) bool {
	return term.IsTerminal(fd)
}

// shouldOutputJSON returns true if the command should produce JSON output.
// This is the case when the --json flag is set explicitly, or when stdout
// is not a terminal (e.g., piped to another program or captured by an agent).
func shouldOutputJSON(cmd *cobra.Command) bool {
	if jsonFlag, _ := cmd.Flags().GetBool("json"); jsonFlag {
		return true
	}
	return !isTerminal(int(os.Stdout.Fd()))
}

type structuredError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type codedError struct {
	code  string
	cause error
}

func (e *codedError) Error() string { return e.cause.Error() }
func (e *codedError) Unwrap() error { return e.cause }
func (e *codedError) Code() string  { return e.code }

func newCodedError(code string, err error) error {
	if err == nil {
		return nil
	}
	return &codedError{code: code, cause: err}
}

type reportedError struct {
	cause error
}

func (e *reportedError) Error() string {
	return e.cause.Error()
}

func (e *reportedError) Unwrap() error {
	return e.cause
}

// ErrorAlreadyReported returns true when the command already emitted its
// structured error output and callers should only preserve the non-zero exit.
func ErrorAlreadyReported(err error) bool {
	var reported *reportedError
	return errors.As(err, &reported)
}

// outputError writes a structured JSON error to stderr when in JSON mode,
// or returns the error for normal Cobra error handling otherwise.
func outputError(cmd *cobra.Command, err error) error {
	if shouldOutputJSON(cmd) {
		msg := struct {
			Error structuredError `json:"error"`
		}{}
		msg.Error.Code = errorCode(err)
		msg.Error.Message = err.Error()
		data, _ := json.Marshal(msg)
		fmt.Fprintln(cmd.ErrOrStderr(), string(data))
		return &reportedError{cause: err}
	}
	return err
}

func errorCode(err error) string {
	type codeCarrier interface{ Code() string }
	var coded codeCarrier
	if errors.As(err, &coded) && coded.Code() != "" {
		return coded.Code()
	}
	return "command_failed"
}
