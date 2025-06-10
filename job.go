package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// job.go
// Redis benchmarking job and runner abstractions

// Job represents a single benchmark operation (can be extended later)
type Job struct {
	Seq int // sequence number for key uniqueness
}

// Result represents the outcome of a single operation (latency in nanoseconds, error if any)
type Result struct {
	Latency time.Duration
	Err     error
}

// JobRunner abstracts the logic for running a job of a specific data type
type JobRunner interface {
	Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error
	RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result
}

// --- Helper Functions ---

// generateKey creates a Redis key for a given runner id and job sequence
func generateKey(id, seq int) string {
	return fmt.Sprintf("bench:%d:%d", id, seq)
}

// generateValue creates a random value of the given size
func generateValue(size int) []byte {
	value := make([]byte, size)
	_, _ = rand.Read(value)
	return value
}

// --- StringJobRunner Implementation ---

type StringJobRunner struct{}

// Run executes a single SET operation with expiry
func (r *StringJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	key := generateKey(id, job.Seq)
	value := generateValue(cfg.ValueSize)
	expiry := calculateExpiry(cfg)
	_, err := rdb.Set(ctx, key, value, expiry).Result()
	return err
}

// RunBatch executes a batch of SET operations using pipelining
func (r *StringJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	pipe := rdb.Pipeline()
	cmds := make([]*redis.StatusCmd, 0, len(jobs))
	sendTimes := make([]time.Time, 0, len(jobs))
	for _, job := range jobs {
		key := generateKey(id, job.Seq)
		value := generateValue(cfg.ValueSize)
		expiry := calculateExpiry(cfg)
		sendTimes = append(sendTimes, time.Now())
		cmd := pipe.Set(ctx, key, value, expiry)
		cmds = append(cmds, cmd)
	}
	_, pipeErr := pipe.Exec(ctx)
	results := make([]Result, len(jobs))
	for i, cmd := range cmds {
		latency := time.Since(sendTimes[i])
		err := pipeErr
		if cmd.Err() != nil {
			err = cmd.Err()
		}
		results[i] = Result{Latency: latency, Err: err}
	}
	return results
}

// --- HashJobRunner Implementation ---

type HashJobRunner struct{}

// Run executes a single HSET (and optional EXPIRE) operation
func (r *HashJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	key := generateKey(id, job.Seq)
	value := generateValue(cfg.ValueSize)
	expiry := calculateExpiry(cfg)
	pipe := rdb.Pipeline()
	pipe.HSet(ctx, key, "field1", value)
	if expiry > 0 {
		pipe.Expire(ctx, key, expiry)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// RunBatch executes a batch of HSET (and optional EXPIRE) operations using pipelining
func (r *HashJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	pipe := rdb.Pipeline()
	startTimes := make([]time.Time, len(jobs))
	cmdsPerJob := make([]int, len(jobs)) // 1 or 2 per job
	for i, job := range jobs {
		key := generateKey(id, job.Seq)
		value := generateValue(cfg.ValueSize)
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

// --- EmptyJobRunner Implementation (No-op) ---

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

// --- UnsupportedJobRunner Implementation ---

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
