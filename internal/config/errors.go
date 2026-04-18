package config

import "errors"

var (
	ErrNoUpstreams = errors.New(
		"at least one upstream required",
	)
	ErrRootWithMultipleUpstreams = errors.New(
		"root prefix \"/\" only allowed with a single upstream",
	)
	ErrNestedPrefixes = errors.New(
		"upstream prefixes must not overlap",
	)
	ErrPrefixConflictsWithJobsPath = errors.New(
		"upstream prefix conflicts with jobs path",
	)
)
