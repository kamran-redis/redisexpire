package main

import (
	"errors"
	"flag"
	"time"
)

type Config struct {
	Host        string
	Port        int
	Password    string
	Concurrency int
	Requests    int
	RPS         int
	Duration    time.Duration
	ValueSize   int
	Expiry      int    // Expiry in seconds for SET keys, 0 means no expiry
	DataType    string // "string" or "hash" or "empty"

	// New fields for absolute expiry
	ExpiryAtRaw string    // Raw input for expiry-at (e.g., "5m")
	ExpiryAt    time.Time // Calculated absolute expiry time

	PipelineSize int // Number of commands per pipeline (1 = no pipelining)
}

func ParseConfig() (*Config, error) {
	cfg := &Config{}

	flag.StringVar(&cfg.Host, "host", "localhost", "Redis server host")
	flag.IntVar(&cfg.Port, "port", 6379, "Redis server port")
	flag.StringVar(&cfg.Password, "password", "", "Redis server password")
	flag.IntVar(&cfg.Concurrency, "concurrency", 50, "Number of concurrent workers")
	flag.IntVar(&cfg.Requests, "requests", 10000, "Total number of requests (ignored if duration is set)")
	flag.IntVar(&cfg.RPS, "rps", 0, "Requests per second (0 = unlimited)")
	flag.DurationVar(&cfg.Duration, "duration", 0, "How long to run the test (e.g., 10s, 1m). If set, overrides requests count.")
	flag.IntVar(&cfg.ValueSize, "value-size", 128, "Size of value in bytes for SET commands")
	flag.IntVar(&cfg.Expiry, "expiry", 0, "Expiry in seconds for SET keys (0 = no expiry)")
	flag.StringVar(&cfg.DataType, "data-type", "string", "Data type to write: string, hash, or empty")
	flag.StringVar(&cfg.ExpiryAtRaw, "expiry-at", "", "Absolute expiry for all keys, as a duration from start (e.g., 5m). Overrides -expiry if set.")
	flag.IntVar(&cfg.PipelineSize, "pipeline-size", 1, "Number of commands per pipeline (1 = no pipelining)")

	flag.Parse()

	if cfg.Concurrency <= 0 {
		return nil, errors.New("concurrency must be > 0")
	}
	if cfg.RPS < 0 {
		return nil, errors.New("rps must be >= 0")
	}
	if cfg.ValueSize < 0 {
		return nil, errors.New("value-size must be >= 0")
	}
	if cfg.Duration == 0 && cfg.Requests <= 0 {
		return nil, errors.New("either duration must be set or requests must be > 0")
	}
	if cfg.Expiry < 0 {
		return nil, errors.New("expiry must be >= 0")
	}
	if cfg.DataType != "string" && cfg.DataType != "hash" && cfg.DataType != "empty" {
		return nil, errors.New("data-type must be 'string', 'hash', or 'empty'")
	}
	if cfg.PipelineSize < 1 {
		return nil, errors.New("pipeline-size must be >= 1")
	}

	// Parse expiry-at if set
	if cfg.ExpiryAtRaw != "" {
		dur, err := time.ParseDuration(cfg.ExpiryAtRaw)
		if err != nil {
			return nil, errors.New("invalid expiry-at duration: " + err.Error())
		}
		cfg.ExpiryAt = time.Now().Add(dur)
	}

	return cfg, nil
}
