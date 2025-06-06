package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	hdr "github.com/HdrHistogram/hdrhistogram-go"
	redis "github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
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

// worker reads jobs from the jobs channel, executes a Redis SET command, and sends results
func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup, cfg *Config) {
	defer wg.Done()
	// Create a Redis client for this worker
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       0,
	})
	ctx := context.Background()

	for job := range jobs {
		key := fmt.Sprintf("bench:%d:%d", id, job.Seq)
		value := make([]byte, cfg.ValueSize)
		_, _ = rand.Read(value)
		var err error
		start := time.Now()
		if cfg.DataType == "string" {
			var expiry time.Duration
			if cfg.Expiry > 0 {
				expiry = time.Duration(cfg.Expiry) * time.Second
			} else {
				expiry = 0
			}
			_, err = rdb.Set(ctx, key, value, expiry).Result()
		} else if cfg.DataType == "hash" {
			_, err = rdb.HSet(ctx, key, "field1", value).Result()
			if err == nil && cfg.Expiry > 0 {
				expiry := time.Duration(cfg.Expiry) * time.Second
				_, err = rdb.Expire(ctx, key, expiry).Result()
			}
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

func printTableHeader() {
	fmt.Printf("%-8s %-10s %-8s %-10s %-10s %-10s %-10s %-10s %-10s\n",
		"Time(s)", "Requests", "Errors", "Min", "Max", "Mean", "P50", "P99", "Throughput")
}

func printPerSecondStats(elapsed time.Duration, count, errorCount, secCount, secError int, secHist *hdr.Histogram) {
	if secCount == 0 {
		fmt.Printf("%-8.1f %-10d %-8d %-10s %-10s %-10s %-10s %-10s %-10.2f\n",
			elapsed.Seconds(), count, errorCount, "-", "-", "-", "-", "-", 0.0)
		return
	}
	secMin := time.Duration(secHist.Min()) * time.Microsecond
	secMax := time.Duration(secHist.Max()) * time.Microsecond
	secMean := time.Duration(secHist.Mean()) * time.Microsecond
	secP50 := time.Duration(secHist.ValueAtQuantile(50)) * time.Microsecond
	secP99 := time.Duration(secHist.ValueAtQuantile(99)) * time.Microsecond
	secThrpt := float64(secCount)
	fmt.Printf("%-8.1f %-10d %-8d %-10v %-10v %-10v %-10v %-10v %-10.2f\n",
		elapsed.Seconds(), count, errorCount, secMin, secMax, secMean, secP50, secP99, secThrpt)
}

func printFinalSummaryRow(elapsed time.Duration, hist *hdr.Histogram, count, errorCount int) {
	if count == 0 {
		fmt.Printf("%-8s %-10d %-8d %-10s %-10s %-10s %-10s %-10s %-10.2f\n",
			"TOTAL", 0, 0, "-", "-", "-", "-", "-", 0.0)
		return
	}
	min := time.Duration(hist.Min()) * time.Microsecond
	max := time.Duration(hist.Max()) * time.Microsecond
	mean := time.Duration(hist.Mean()) * time.Microsecond
	p50 := time.Duration(hist.ValueAtQuantile(50)) * time.Microsecond
	p99 := time.Duration(hist.ValueAtQuantile(99)) * time.Microsecond
	throughput := float64(count) / elapsed.Seconds()
	fmt.Printf("%-8s %-10d %-8d %-10v %-10v %-10v %-10v %-10v %-10.2f\n",
		"TOTAL", count, errorCount, min, max, mean, p50, p99, throughput)
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

	// Accurate rate limiting using token bucket
	var limiter *rate.Limiter
	if cfg.RPS > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.RPS), 1)
	}

	// Dispatch jobs
	if cfg.Duration > 0 {
		fmt.Printf("Running for duration: %v\n", cfg.Duration)
		timer := time.After(cfg.Duration)
		seq := 0
	loop:
		for {
			select {
			case <-timer:
				break loop
			default:
				if limiter != nil {
					_ = limiter.Wait(context.Background())
				}
				jobs <- Job{Seq: seq}
				seq++
			}
		}
	} else {
		for i := 0; i < totalRequests; i++ {
			if limiter != nil {
				_ = limiter.Wait(context.Background())
			}
			jobs <- Job{Seq: i}
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
