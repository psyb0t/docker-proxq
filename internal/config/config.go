package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	commonerrors "github.com/psyb0t/common-go/errors"
	"github.com/psyb0t/ctxerrors"
	"gopkg.in/yaml.v3"
)

const (
	DefaultListenAddress              = "127.0.0.1:8080"
	DefaultRedisAddr                  = "127.0.0.1:6379"
	DefaultQueue                      = "default"
	DefaultConcurrency                = 10
	DefaultJobsPath                   = "/__jobs"
	DefaultTaskRetention              = Duration(1 * time.Hour)
	DefaultUpstreamTimeout            = Duration(5 * time.Minute)
	DefaultMaxBodySize          int64 = 10 << 20 // 10MB
	DefaultDirectProxyThreshold int64 = 10 << 20
	DefaultCacheMaxEntries            = 10000
	DefaultCacheRedisKeyPrefix        = "proxq:"
	DefaultCacheTTL                   = Duration(5 * time.Minute)
)

type CacheMode = string

const (
	CacheModeNone   CacheMode = "none"
	CacheModeMemory CacheMode = "memory"
	CacheModeRedis  CacheMode = "redis"
)

type DirectProxyMode = string

const (
	DirectProxyModeProxy    DirectProxyMode = "proxy"
	DirectProxyModeRedirect DirectProxyMode = "redirect"
)

type PathFilterMode = string

const (
	PathFilterModeBlacklist PathFilterMode = "blacklist"
	PathFilterModeWhitelist PathFilterMode = "whitelist"
)

type Duration time.Duration

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d *Duration) UnmarshalYAML(
	value *yaml.Node,
) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return ctxerrors.Wrap(err, "decode duration")
	}

	parsed, err := time.ParseDuration(s)
	if err != nil {
		return ctxerrors.Wrap(
			err, "parse duration: "+s,
		)
	}

	*d = Duration(parsed)

	return nil
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type CacheConfig struct {
	Mode           string   `yaml:"mode"`
	TTL            Duration `yaml:"ttl"`
	MaxEntries     int      `yaml:"maxEntries"`
	RedisKeyPrefix string   `yaml:"redisKeyPrefix"`
}

type PathFilterConfig struct {
	Mode     string   `yaml:"mode"`
	Patterns []string `yaml:"patterns"`
}

type Upstream struct {
	Prefix               string           `yaml:"prefix"`
	URL                  string           `yaml:"url"`
	Timeout              Duration         `yaml:"timeout"`
	MaxRetries           int              `yaml:"maxRetries"`
	RetryDelay           Duration         `yaml:"retryDelay"`
	MaxBodySize          int64            `yaml:"maxBodySize"`
	DirectProxyThreshold int64            `yaml:"directProxyThreshold"` //nolint:lll
	DirectProxyMode      string           `yaml:"directProxyMode"`
	PathFilter           PathFilterConfig `yaml:"pathFilter"`

	CompiledPatterns []*regexp.Regexp `yaml:"-"`
}

type Config struct {
	ListenAddress string      `yaml:"listenAddress"`
	Redis         RedisConfig `yaml:"redis"`
	Queue         string      `yaml:"queue"`
	Concurrency   int         `yaml:"concurrency"`
	JobsPath      string      `yaml:"jobsPath"`
	TaskRetention Duration    `yaml:"taskRetention"`
	Cache         CacheConfig `yaml:"cache"`
	Upstreams     []Upstream  `yaml:"upstreams"`
}

func Parse(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, ctxerrors.Wrap(
			err, "read config file",
		)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, ctxerrors.Wrap(
			err, "parse config yaml",
		)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return Config{}, err
	}

	sortUpstreams(cfg.Upstreams)

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	applyGlobalDefaults(cfg)
	applyCacheDefaults(&cfg.Cache)

	for i := range cfg.Upstreams {
		applyUpstreamDefaults(&cfg.Upstreams[i])
	}
}

func applyGlobalDefaults(cfg *Config) {
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = DefaultListenAddress
	}

	if cfg.Redis.Addr == "" {
		cfg.Redis.Addr = DefaultRedisAddr
	}

	if cfg.Queue == "" {
		cfg.Queue = DefaultQueue
	}

	if cfg.Concurrency == 0 {
		cfg.Concurrency = DefaultConcurrency
	}

	if cfg.JobsPath == "" {
		cfg.JobsPath = DefaultJobsPath
	}

	if cfg.TaskRetention == 0 {
		cfg.TaskRetention = DefaultTaskRetention
	}
}

func applyCacheDefaults(c *CacheConfig) {
	if c.Mode == "" {
		c.Mode = CacheModeNone
	}

	if c.TTL == 0 {
		c.TTL = DefaultCacheTTL
	}

	if c.MaxEntries == 0 {
		c.MaxEntries = DefaultCacheMaxEntries
	}

	if c.RedisKeyPrefix == "" {
		c.RedisKeyPrefix = DefaultCacheRedisKeyPrefix
	}
}

func applyUpstreamDefaults(u *Upstream) {
	if u.Timeout == 0 {
		u.Timeout = DefaultUpstreamTimeout
	}

	if u.MaxBodySize == 0 {
		u.MaxBodySize = DefaultMaxBodySize
	}

	if u.DirectProxyThreshold == 0 {
		u.DirectProxyThreshold = DefaultDirectProxyThreshold
	}

	if u.DirectProxyMode == "" {
		u.DirectProxyMode = DirectProxyModeProxy
	}

	if u.PathFilter.Mode == "" {
		u.PathFilter.Mode = PathFilterModeBlacklist
	}
}

func validate(cfg *Config) error {
	if len(cfg.Upstreams) == 0 {
		return ErrNoUpstreams
	}

	for i := range cfg.Upstreams {
		u := &cfg.Upstreams[i]

		if u.Prefix == "" {
			return ctxerrors.Wrap(
				commonerrors.ErrRequiredFieldNotSet,
				"upstream prefix",
			)
		}

		if u.URL == "" {
			return ctxerrors.Wrap(
				commonerrors.ErrRequiredFieldNotSet,
				"upstream url for prefix: "+u.Prefix,
			)
		}

		compiled, err := compilePatterns(
			u.PathFilter.Patterns,
		)
		if err != nil {
			return ctxerrors.Wrap(
				err,
				"compile path filter for prefix: "+
					u.Prefix,
			)
		}

		u.CompiledPatterns = compiled
	}

	if err := validatePrefixes(
		cfg.Upstreams, cfg.JobsPath,
	); err != nil {
		return err
	}

	return nil
}

func validatePrefixes(
	upstreams []Upstream,
	jobsPath string,
) error {
	if err := validateJobsPathConflict(
		upstreams, jobsPath,
	); err != nil {
		return err
	}

	if len(upstreams) <= 1 {
		return nil
	}

	return validateMultipleUpstreams(upstreams)
}

func validateJobsPathConflict(
	upstreams []Upstream,
	jobsPath string,
) error {
	for _, u := range upstreams {
		if u.Prefix == "/" {
			continue
		}

		if u.Prefix == jobsPath ||
			strings.HasPrefix(jobsPath, u.Prefix+"/") ||
			strings.HasPrefix(u.Prefix, jobsPath+"/") {
			return ctxerrors.Wrap(
				ErrPrefixConflictsWithJobsPath,
				u.Prefix+" conflicts with "+jobsPath,
			)
		}
	}

	return nil
}

func validateMultipleUpstreams(
	upstreams []Upstream,
) error {
	for _, u := range upstreams {
		if u.Prefix == "/" {
			return ErrRootWithMultipleUpstreams
		}
	}

	for i, a := range upstreams {
		for j, b := range upstreams {
			if i == j {
				continue
			}

			if strings.HasPrefix(
				a.Prefix+"/", b.Prefix+"/",
			) {
				return ctxerrors.Wrap(
					ErrNestedPrefixes,
					a.Prefix+" overlaps "+b.Prefix,
				)
			}
		}
	}

	return nil
}

func compilePatterns(
	patterns []string,
) ([]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))

	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, ctxerrors.Wrap(
				err, "compile regex: "+p,
			)
		}

		compiled = append(compiled, re)
	}

	return compiled, nil
}

func sortUpstreams(upstreams []Upstream) {
	sort.Slice(upstreams, func(i, j int) bool {
		return len(upstreams[i].Prefix) >
			len(upstreams[j].Prefix)
	})
}
