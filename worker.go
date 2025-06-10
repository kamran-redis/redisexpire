package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	hdr "github.com/HdrHistogram/hdrhistogram-go"
	redis "github.com/redis/go-redis/v9"
)

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
