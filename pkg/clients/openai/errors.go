package openai

import "errors"

var (
	ErrEmptyJobID = errors.New(
		"proxq returned empty job id",
	)
	ErrJobFailed = errors.New(
		"proxq job failed",
	)
)
