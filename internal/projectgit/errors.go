package projectgit

import (
	"errors"
	"fmt"
)

const (
	CodeDiffFailed            = "git_diff_failed"
	CodeGitUnavailable        = "git_unavailable"
	CodeInvalidPath           = "path_not_authorized"
	CodeNotRepository         = "not_git_repository"
	CodeOutputTooLarge        = "git_output_too_large"
	CodeParseFailed           = "git_parse_failed"
	CodeRepositoryOutsideRoot = "repository_outside_project"
	CodeStatusFailed          = "git_status_failed"
	CodeTimedOut              = "timed_out"
)

type Error struct {
	Code   string
	Detail string
	Err    error
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error { return e.Err }

func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func newError(code, detail string, err error) error {
	if detail == "" && err != nil {
		detail = err.Error()
	}
	return &Error{Code: code, Detail: detail, Err: err}
}

func parseError(format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	return newError(CodeParseFailed, detail, nil)
}
