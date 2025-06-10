package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// job.go
// Contains Job, Result, JobRunner interface, and all JobRunner implementations for the Redis benchmarking tool.
// Provides the abstraction and logic for different Redis data types and job execution.

// Job represents a single benchmark operation (can be extended later)
type Job struct {
	Seq int // sequence number for key uniqueness
}

// Result represents the outcome of a single operation
// (latency in nanoseconds, error if any)
type Result struct {
	Latency time.Duration
	Err     error
}

// JobRunner abstracts the logic for running a job of a specific data type
type JobRunner interface {
	Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error
	RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result
}

// StringJobRunner implements JobRunner for string data type
type StringJobRunner struct{}

func (r *StringJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	key := fmt.Sprintf("bench:%d:%d", id, job.Seq)
	value := make([]byte, cfg.ValueSize)
	_, _ = rand.Read(value)
	expiry := calculateExpiry(cfg)
	_, err := rdb.Set(ctx, key, value, expiry).Result()
	return err
}

func (r *StringJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	pipe := rdb.Pipeline()
	cmds := make([]*redis.StatusCmd, 0, len(jobs))
	sendTimes := make([]time.Time, 0, len(jobs))
	for _, job := range jobs {
		key := fmt.Sprintf("bench:%d:%d", id, job.Seq)
		value := make([]byte, cfg.ValueSize)
		_, _ = rand.Read(value)
		expiry := calculateExpiry(cfg)
		sendTimes = append(sendTimes, time.Now())
		cmd := pipe.Set(ctx, key, value, expiry)
		cmds = append(cmds, cmd)
	}
	_, err := pipe.Exec(ctx)
	results := make([]Result, len(jobs))
	for i, cmd := range cmds {
		latency := time.Since(sendTimes[i])
		cmdErr := err
		if cmd.Err() != nil {
			cmdErr = cmd.Err()
		}
		results[i] = Result{Latency: latency, Err: cmdErr}
	}
	return results
}

// HashJobRunner implements JobRunner for hash data type
type HashJobRunner struct{}

func (r *HashJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	key := fmt.Sprintf("bench:%d:%d", id, job.Seq)
	value := make([]byte, cfg.ValueSize)
	_, _ = rand.Read(value)
	expiry := calculateExpiry(cfg)
	pipe := rdb.Pipeline()
	pipe.HSet(ctx, key, "field1", value).Result()
	if expiry > 0 {
		pipe.Expire(ctx, key, expiry).Result()
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (r *HashJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	pipe := rdb.Pipeline()
	startTimes := make([]time.Time, len(jobs))
	cmdsPerJob := make([]int, len(jobs)) // 1 or 2 per job
	for i, job := range jobs {
		key := fmt.Sprintf("bench:%d:%d", id, job.Seq)
		value := make([]byte, cfg.ValueSize)
		_, _ = rand.Read(value)
		expiry := calculateExpiry(cfg)
		startTimes[i] = time.Now()
		pipe.HSet(ctx, key, "field1", value)
		cmdsPerJob[i] = 1
		if expiry > 0 {
			pipe.Expire(ctx, key, expiry)
			cmdsPerJob[i] = 2
		}
	}
	cmds, _ := pipe.Exec(ctx)
	results := make([]Result, len(jobs))
	idx := 0
	for i := range jobs {
		var jobErr error
		for j := 0; j < cmdsPerJob[i]; j++ {
			if cmds[idx].Err() != nil {
				if jobErr == nil {
					jobErr = cmds[idx].Err()
				} else {
					jobErr = fmt.Errorf("%v; %v", jobErr, cmds[idx].Err())
				}
			}
			idx++
		}
		latency := time.Since(startTimes[i])
		results[i] = Result{Latency: latency, Err: jobErr}
	}
	return results
}

// EmptyJobRunner implements JobRunner for empty data type (no-op)
type EmptyJobRunner struct{}

func (r *EmptyJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	return nil
}

func (r *EmptyJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	results := make([]Result, len(jobs))
	for i := range jobs {
		results[i] = Result{Latency: 0, Err: nil}
	}
	return results
}

// unsupportedJobRunner always returns an error for unsupported data types
type unsupportedJobRunner struct {
	dataType string
}

func (r *unsupportedJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	return fmt.Errorf("unsupported data type: %s", r.dataType)
}

func (r *unsupportedJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	results := make([]Result, len(jobs))
	for i := range jobs {
		results[i] = Result{Latency: 0, Err: fmt.Errorf("unsupported data type: %s", r.dataType)}
	}
	return results
}
