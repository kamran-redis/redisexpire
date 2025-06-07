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

	for job := range jobs {
		key := fmt.Sprintf("bench:%d:%d", id, job.Seq)
		value := make([]byte, cfg.ValueSize)
		_, _ = rand.Read(value)
		var err error
		start := time.Now()

		expiry := calculateExpiry(cfg)

		switch cfg.DataType {
		case "string":
			_, err = rdb.Set(ctx, key, value, expiry).Result()
		case "hash":
			_, err = rdb.HSet(ctx, key, "field1", value).Result()
			if err == nil && expiry > 0 {
				_, err = rdb.Expire(ctx, key, expiry).Result()
			}
		default:
			err = fmt.Errorf("unsupported data type: %s", cfg.DataType)
		}

		latency := time.Since(start)
		results <- Result{Latency: latency, Err: err}
	}
	_ = rdb.Close()
}

// MetricsRow holds all the metrics for a single reporting row.
type MetricsRow struct {
	TimeLabel  string
	Requests   int
	Errors     int
	Min        string
	Max        string
	Mean       string
	P50        string
	P99        string
	P999       string
	P9999      string
	Throughput float64
}

// formatMetricsRow returns a formatted string for a metrics row.
func formatMetricsRow(row MetricsRow) string {
	return fmt.Sprintf("%-8s %-10d %-8d %-10s %-10s %-10s %-10s %-10s %-10s %-10s %-10.2f",
		row.TimeLabel, row.Requests, row.Errors, row.Min, row.Max, row.Mean, row.P50, row.P99, row.P999, row.P9999, row.Throughput)
}

// printTableHeader prints the column headers for the metrics table.
func printTableHeader() {
	headers := []string{"Time(s)", "Requests", "Errors", "Min", "Max", "Mean", "P50", "P99", "P99.9", "P99.99", "Throughput"}
	fmt.Printf("%-8s %-10s %-8s %-10s %-10s %-10s %-10s %-10s %-10s %-10s %-10s\n",
		headers[0], headers[1], headers[2], headers[3], headers[4], headers[5], headers[6], headers[7], headers[8], headers[9], headers[10])
}

// printPerSecondStats prints the per-second metrics row.
func printPerSecondStats(elapsed time.Duration, count, errorCount, secCount, secError int, secHist *hdr.Histogram) {
	row := MetricsRow{
		TimeLabel:  fmt.Sprintf("%.1f", elapsed.Seconds()),
		Requests:   count,
		Errors:     errorCount,
		Throughput: float64(secCount),
	}
	if secCount == 0 {
		row.Min, row.Max, row.Mean, row.P50, row.P99, row.P999, row.P9999 = "-", "-", "-", "-", "-", "-", "-"
	} else {
		row.Min = (time.Duration(secHist.Min()) * time.Microsecond).String()
		row.Max = (time.Duration(secHist.Max()) * time.Microsecond).String()
		row.Mean = (time.Duration(secHist.Mean()) * time.Microsecond).String()
		row.P50 = (time.Duration(secHist.ValueAtQuantile(50)) * time.Microsecond).String()
		row.P99 = (time.Duration(secHist.ValueAtQuantile(99)) * time.Microsecond).String()
		row.P999 = (time.Duration(secHist.ValueAtQuantile(99.9)) * time.Microsecond).String()
		row.P9999 = (time.Duration(secHist.ValueAtQuantile(99.99)) * time.Microsecond).String()
	}
	fmt.Println(formatMetricsRow(row))
}

// printFinalSummaryRow prints the final summary metrics row.
func printFinalSummaryRow(elapsed time.Duration, hist *hdr.Histogram, count, errorCount int) {
	row := MetricsRow{
		TimeLabel:  "TOTAL",
		Requests:   count,
		Errors:     errorCount,
		Throughput: 0.0,
	}
	if count == 0 {
		row.Min, row.Max, row.Mean, row.P50, row.P99, row.P999, row.P9999 = "-", "-", "-", "-", "-", "-", "-"
	} else {
		row.Min = (time.Duration(hist.Min()) * time.Microsecond).String()
		row.Max = (time.Duration(hist.Max()) * time.Microsecond).String()
		row.Mean = (time.Duration(hist.Mean()) * time.Microsecond).String()
		row.P50 = (time.Duration(hist.ValueAtQuantile(50)) * time.Microsecond).String()
		row.P99 = (time.Duration(hist.ValueAtQuantile(99)) * time.Microsecond).String()
		row.P999 = (time.Duration(hist.ValueAtQuantile(99.9)) * time.Microsecond).String()
		row.P9999 = (time.Duration(hist.ValueAtQuantile(99.99)) * time.Microsecond).String()
		row.Throughput = float64(count) / elapsed.Seconds()
	}
	fmt.Println(formatMetricsRow(row))
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
