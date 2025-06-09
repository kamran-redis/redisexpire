package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	hdr "github.com/HdrHistogram/hdrhistogram-go"
	redis "github.com/redis/go-redis/v9"
)

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

// calculateExpiry returns the expiry duration to use for a key, based on config and current time.
// Always returns at least 1 second if expiry is required.
func calculateExpiry(cfg *Config) time.Duration {
	if cfg.ExpiryAtRaw != "" {
		delta := cfg.ExpiryAt.Sub(time.Now())
		if delta > time.Second {
			return delta
		}
		// If delta is <= 1s or negative, always set to 1s (minimum allowed)
		return time.Second
	}
	if cfg.Expiry > 0 {
		return time.Duration(cfg.Expiry) * time.Second
	}
	return 0
}

// JobRunner abstracts the logic for running a job of a specific data type
type JobRunner interface {
	Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error
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

// EmptyJobRunner implements JobRunner for empty data type (no-op)
type EmptyJobRunner struct{}

func (r *EmptyJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	// No-op
	return nil
}

// unsupportedJobRunner always returns an error for unsupported data types
type unsupportedJobRunner struct {
	dataType string
}

func (r *unsupportedJobRunner) Run(ctx context.Context, rdb *redis.Client, id int, job Job, cfg *Config) error {
	return fmt.Errorf("unsupported data type: %s", r.dataType)
}

// worker reads jobs from the jobs channel, executes a Redis SET or HSET command, and sends results
func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup, cfg *Config) {
	defer wg.Done()
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       0,
	})
	ctx := context.Background()

	// Select the appropriate JobRunner based on cfg.DataType
	var runner JobRunner
	switch cfg.DataType {
	case "string":
		runner = &StringJobRunner{}
	case "hash":
		runner = &HashJobRunner{}
	case "empty":
		runner = &EmptyJobRunner{}
	default:
		// If unsupported, use a runner that always returns error
		runner = &unsupportedJobRunner{dataType: cfg.DataType}
	}

	for job := range jobs {
		start := time.Now()
		err := runner.Run(ctx, rdb, id, job, cfg)
		if err != nil {
			fmt.Printf("[worker %d] error on job %d: %v\n", id, job.Seq, err)
		}
		latency := time.Since(start)
		results <- Result{Latency: latency, Err: err}
	}
	_ = rdb.Close()
}

// metricsReporter aggregates and prints real-time metrics from the results channel
func metricsReporter(results <-chan Result, done chan<- struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var count int
	var errorCount int
	startTime := time.Now()

	// Cumulative histogram for final summary
	hist := hdr.New(1, 10_000_000_000, 3)

	headerEvery := 10
	headerCount := 0

	// Per-second stats
	perSecHist := hdr.New(1, 10_000_000_000, 3)
	perSecCount := 0
	perSecError := 0

	for {
		select {
		case r, ok := <-results:
			if !ok {
				// Final report
				elapsed := time.Since(startTime)
				printTableHeader()
				printFinalSummaryRow(elapsed, hist, count, errorCount)
				done <- struct{}{}
				return
			}
			count++
			if r.Err != nil {
				errorCount++
			}
			hist.RecordValue(r.Latency.Microseconds())
			perSecHist.RecordValue(r.Latency.Microseconds())
			perSecCount++
			if r.Err != nil {
				perSecError++
			}
		case <-ticker.C:
			elapsed := time.Since(startTime)
			headerCount++
			if headerCount%headerEvery == 1 {
				printTableHeader()
			}
			printPerSecondStats(elapsed, count, errorCount, perSecCount, perSecError, perSecHist)
			perSecHist.Reset()
			perSecCount = 0
			perSecError = 0
		}
	}
}

// runWorkerPool starts the worker pool, dispatches jobs, and collects results
func runWorkerPool(concurrency, totalRequests int, cfg *Config) []Result {
	var jobs chan Job
	if cfg.Duration > 0 {
		jobs = make(chan Job, 1000)
	} else {
		bufSize := totalRequests
		if bufSize > 1000 {
			bufSize = 1000
		}
		if bufSize < 1 {
			bufSize = 1
		}
		jobs = make(chan Job, bufSize)
	}
	results := make(chan Result, totalRequests)
	var wg sync.WaitGroup
	done := make(chan struct{})

	// Start metrics reporter
	go metricsReporter(results, done)

	// Start workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg, cfg)
	}

	// Dispatch jobs
	if cfg.Duration > 0 {
		fmt.Printf("Running for duration: %v\n", cfg.Duration)
		end := time.Now().Add(cfg.Duration)
		seq := 0
		if cfg.RPS > 0 {
			interval := time.Second / time.Duration(cfg.RPS)
			next := time.Now()
			for time.Now().Before(end) {
				now := time.Now()
				if now.Before(next) {
					time.Sleep(next.Sub(now))
				}
				jobs <- Job{Seq: seq}
				seq++
				next = next.Add(interval)
			}
		} else {
			// No RPS specified: fire as fast as possible for the duration
			for time.Now().Before(end) {
				jobs <- Job{Seq: seq}
				seq++
			}
		}
	} else {
		if cfg.RPS > 0 {
			interval := time.Second / time.Duration(cfg.RPS)
			next := time.Now()
			for i := 0; i < totalRequests; i++ {
				now := time.Now()
				if now.Before(next) {
					time.Sleep(next.Sub(now))
				}
				jobs <- Job{Seq: i}
				next = next.Add(interval)
			}
		} else {
			for i := 0; i < totalRequests; i++ {
				jobs <- Job{Seq: i}
			}
		}
	}
	close(jobs)

	// Wait for workers to finish
	wg.Wait()
	close(results)

	// Wait for metrics reporter to finish
	<-done

	// (Optional) Return empty slice for now, as metrics are reported in real time
	return nil
}
