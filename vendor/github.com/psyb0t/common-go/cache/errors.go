package cache

import "errors"

var (
	ErrCacheMiss        = errors.New("cache miss")
	ErrInvalidCacheMode = errors.New("invalid cache mode")
)
