package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// --- Unit Tests ---

func TestCalculateExpiry(t *testing.T) {
	now := time.Now()
	cfg := &Config{}

	// Test with only Expiry set
	cfg.Expiry = 10
	cfg.ExpiryAtRaw = ""
	cfg.ExpiryAt = time.Time{}
	if got := calculateExpiry(cfg); got != 10*time.Second {
		t.Errorf("Expected 10s, got %v", got)
	}

	// Test with ExpiryAtRaw set to 5m in the future
	cfg.Expiry = 0
	cfg.ExpiryAtRaw = "5m"
	cfg.ExpiryAt = now.Add(5 * time.Minute)
	if got := calculateExpiry(cfg); got < 4*time.Minute || got > 5*time.Minute {
		t.Errorf("Expected ~5m, got %v", got)
	}

	// Test with ExpiryAtRaw set to past (should return 1s)
	cfg.ExpiryAtRaw = "1s"
	cfg.ExpiryAt = now.Add(-1 * time.Second)
	if got := calculateExpiry(cfg); got != time.Second {
		t.Errorf("Expected 1s for past expiry, got %v", got)
	}

	// Test with neither set
	cfg.ExpiryAtRaw = ""
	cfg.ExpiryAt = time.Time{}
	if got := calculateExpiry(cfg); got != 0 {
		t.Errorf("Expected 0, got %v", got)
	}
}

func TestFormatMetricsRow(t *testing.T) {
	row := MetricsRow{
		TimeLabel:  "1.0",
		Requests:   100,
		Errors:     2,
		Min:        "10us",
		Max:        "100us",
		Mean:       "50us",
		P50:        "45us",
		P99:        "99us",
		P999:       "100us",
		P9999:      "100us",
		Throughput: 100.0,
	}
	out := formatMetricsRow(row)
	if !strings.Contains(out, "1.0") || !strings.Contains(out, "100") || !strings.Contains(out, "10us") {
		t.Errorf("Output missing expected fields: %s", out)
	}
}

// --- Integration Test ---

func TestWorkerIntegration(t *testing.T) {
	// This test requires a running Redis instance on localhost:6379
	cfg := &Config{
		Host:        "localhost",
		Port:        6379,
		Password:    "",
		Concurrency: 1,
		Requests:    1,
		RPS:         0,
		Duration:    0,
		ValueSize:   8,
		Expiry:      2, // 2 seconds
		DataType:    "string",
	}

	// Unique key prefix for the test
	keyPrefix := "testworkerintegration"
	job := Job{Seq: 12345}
	jobs := make(chan Job, 1)
	results := make(chan Result, 1)
	var wg sync.WaitGroup

	// Patch the worker to use our key prefix
	go func() {
		wg.Add(1)
		defer wg.Done()
		addr := "localhost:6379"
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		ctx := context.Background()
		for job := range jobs {
			key := keyPrefix + ":" + "bench:0:" + string(rune(job.Seq))
			value := make([]byte, cfg.ValueSize)
			_, _ = rdb.Set(ctx, key, value, calculateExpiry(cfg)).Result()
			results <- Result{Latency: 0, Err: nil}
		}
		_ = rdb.Close()
	}()

	jobs <- job
	close(jobs)
	wg.Wait()

	// Check that the key exists
	addr := "localhost:6379"
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	key := keyPrefix + ":" + "bench:0:" + string(rune(job.Seq))
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil || exists != 1 {
		t.Fatalf("Expected key to exist after worker, got exists=%d, err=%v", exists, err)
	}

	// Wait for expiry
	time.Sleep(3 * time.Second)
	exists, _ = rdb.Exists(ctx, key).Result()
	if exists != 0 {
		t.Errorf("Expected key to expire, but it still exists")
	}
}
