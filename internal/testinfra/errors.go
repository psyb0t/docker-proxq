package testinfra

import "errors"

var errUnexpectedRedisOptType = errors.New(
	"unexpected redis option type",
)
