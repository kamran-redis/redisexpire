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

// Implement RunBatch for StringJobRunner
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

// Implement RunBatch for HashJobRunner
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

// Implement RunBatch for EmptyJobRunner and unsupportedJobRunner
func (r *EmptyJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	results := make([]Result, len(jobs))
	for i := range jobs {
		results[i] = Result{Latency: 0, Err: nil}
	}
	return results
}

func (r *unsupportedJobRunner) RunBatch(ctx context.Context, rdb *redis.Client, id int, jobs []Job, cfg *Config) []Result {
	results := make([]Result, len(jobs))
	for i := range jobs {
		results[i] = Result{Latency: 0, Err: fmt.Errorf("unsupported data type: %s", r.dataType)}
	}
	return results
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

	var runner JobRunner
	switch cfg.DataType {
	case "string":
		runner = &StringJobRunner{}
	case "hash":
		runner = &HashJobRunner{}
	case "empty":
		runner = &EmptyJobRunner{}
	default:
		runner = &unsupportedJobRunner{dataType: cfg.DataType}
	}

	batchSize := 1
	if cfg.Pipeline > 0 {
		batchSize = cfg.Pipeline
	}
	batch := make([]Job, 0, batchSize)
	for {
		batch = batch[:0]
		for len(batch) < batchSize {
			job, ok := <-jobs
			if !ok {
				break
			}
			batch = append(batch, job)
		}
		if len(batch) == 0 {
			break
		}

		// If batch size is 1, use the simpler non-batch method for efficiency and accuracy
		if batchSize == 1 {
			start := time.Now()
			err := runner.Run(ctx, rdb, id, batch[0], cfg)
			latency := time.Since(start)
			results <- Result{Latency: latency, Err: err}
			continue
		}

		// Otherwise, use batch pipelining
		resultsBatch := runner.RunBatch(ctx, rdb, id, batch, cfg)
		for _, res := range resultsBatch {
			results <- res
		}
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
