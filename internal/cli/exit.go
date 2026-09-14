package cli

import "fmt"

const (
	ExitOK           = 0
	ExitIncompatible = 1
	ExitErrorCode    = 2
)

// ExitError carries a process exit code.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Msg
}

func errExit(code int, format string, args ...any) *ExitError {
	return &ExitError{Code: code, Msg: fmt.Sprintf(format, args...)}
}
