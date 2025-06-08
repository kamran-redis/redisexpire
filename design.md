# RedisExpire Benchmark Tool: Detailed Design Document

## Table of Contents
- [Overview](#overview)
- [Architecture](#architecture)
- [Main Components](#main-components)
- [Key Design Decisions](#key-design-decisions)
- [How It Works (Step by Step)](#how-it-works-step-by-step)
- [Extending the Tool](#extending-the-tool)
- [Learning Go: Resources and Pointers](#learning-go-resources-and-pointers)

---

## Overview

**RedisExpire** is a high-performance benchmarking tool for [Redis](https://redis.io/), written in [Go (Golang)](https://go.dev/). It is designed to stress-test Redis servers by sending a large number of commands (SET, HSET, etc.) with optional expiry, using multiple concurrent workers. The tool provides real-time and summary statistics about request latency, throughput, and error rates.

**Key Features:**
- Configurable concurrency (number of workers)
- Configurable total requests or test duration
- Optional requests-per-second (RPS) rate limiting
- Support for string and hash data types
- Optional key expiry (relative or absolute)
- Real-time and summary metrics (latency, throughput, percentiles, errors)

---

## Architecture

The tool is structured as a command-line application with the following high-level architecture:

```
+-------------------+
|   Main Program    |
+-------------------+
          |
          v
+-------------------+
|  Config Parsing   |
+-------------------+
          |
          v
+-------------------+
|  Worker Pool      |<-------------------+
+-------------------+                    |
          |                              |
          v                              |
+-------------------+         +-------------------+
|  Job Dispatcher   |-------->| Metrics Reporter  |
+-------------------+         +-------------------+
```

- **Main Program**: Entry point, parses config, starts the benchmark.
- **Config Parsing**: Reads and validates command-line flags.
- **Worker Pool**: Launches multiple workers (goroutines), each with its own Redis connection.
- **Job Dispatcher**: Distributes jobs to workers, optionally rate-limited.
- **Metrics Reporter**: Aggregates and prints real-time and summary statistics.

---

## Main Components

### 1. `main.go`
- Entry point. Parses configuration, prints config, and starts the worker pool.
- Calls `ParseConfig()` to read command-line flags.
- Calls `runWorkerPool()` to start the benchmark.

### 2. `config.go`
- Defines the `Config` struct for all parameters (host, port, concurrency, requests, duration, RPS, value size, expiry, data type, etc.).
- Implements `ParseConfig()` to parse and validate command-line flags.
- Handles both relative (`-expiry`) and absolute (`-expiry-at`) expiry options.

### 3. `worker.go`
- Implements the worker pool pattern using Go's goroutines and channels.
- Each worker maintains its own Redis client connection for efficiency.
- Supports three data types: `string`, `hash`, and `empty` (no-op for baseline measurement).
- Handles key expiry logic.
- Collects per-operation results (latency, error).
- Implements job dispatching with optional RPS rate limiting.
- Uses a metrics reporter goroutine to aggregate and print real-time stats.

### 4. `report.go`
- Formats and prints metrics tables (per-second and summary rows).
- Uses [HdrHistogram](https://github.com/HdrHistogram/hdrhistogram-go) for accurate latency percentiles.

### 5. `worker_test.go`
- Contains unit and integration tests for expiry logic, metrics formatting, and worker behavior.

---

## Key Design Decisions

### Worker Pool Model
- Uses Go's goroutines and channels to simulate high concurrency.
- Each worker has its own Redis connection to avoid contention.

### Configurability
- All parameters are exposed as command-line flags for flexibility.
- Supports both fixed request count and fixed duration modes.

### Rate Limiting
- Implements RPS limiting using a simple interval-based approach: the dispatcher sleeps between job submissions to maintain the desired requests-per-second rate.
- This is **not** a token bucket algorithm and does **not** use Go's `golang.org/x/time/rate` package.
- The approach ensures even spacing between requests but does not allow for bursts or token accumulation.

### Real-Time Metrics
- A dedicated goroutine aggregates and prints metrics every second.
- Uses HdrHistogram for accurate latency percentiles.

### Data Type and Expiry Support
- Supports both string and hash data types.
- Optional expiry for benchmarking Redis' expiration mechanism.
- `empty` data type for measuring framework overhead.

### Simplicity and Extensibility
- Modular codebase, easy to extend (e.g., add new Redis commands or metrics).
- Error handling and input validation included.

---

## How It Works (Step by Step)

1. **Startup**: User runs the tool with desired flags (see `README.md` for examples).
2. **Config Parsing**: Flags are parsed and validated. Expiry options are resolved.
3. **Worker Pool Launch**: N workers (goroutines) are started, each with its own Redis connection.
4. **Job Dispatching**: Jobs are sent to workers via a channel. If RPS is set, jobs are rate-limited.
5. **Operation Execution**: Each worker performs the requested Redis command (SET/HSET/empty) and records latency/errors.
6. **Metrics Reporting**: A separate goroutine aggregates results and prints real-time stats every second.
7. **Completion**: After all jobs are done (or duration elapsed), a summary is printed.

---

## Extending the Tool

- **Add new Redis commands**: Extend the `worker` function in `worker.go`.
- **Add new metrics**: Update `report.go` to include new statistics.
- **Support more data types**: Add cases in the worker logic.
- **Improve reporting**: Enhance the metrics reporter for more detailed output.

---

## Learning Go: Resources and Pointers

If you are new to Go, here are some excellent resources to get started:

- [Go Official Site](https://go.dev/): Main site for downloads, docs, and news.
- [A Tour of Go](https://go.dev/tour/): Interactive introduction to Go basics.
- [Go by Example](https://gobyexample.com/): Hands-on annotated Go examples.
- [Go User Manual](https://go.dev/doc/): Comprehensive documentation.
- [Effective Go](https://go.dev/doc/effective_go): Tips for writing clear, idiomatic Go code.
- [Go Concurrency Patterns](https://go.dev/blog/pipelines): Blog post on Go's concurrency model.
- [Go Redis Client](https://github.com/redis/go-redis): Official Redis client for Go (see README for usage examples).
- [Go Rate Limiting (golang.org/x/time/rate)](https://pkg.go.dev/golang.org/x/time/rate): Official docs for the rate limiter used in this tool.
- [HdrHistogram for Go](https://github.com/HdrHistogram/hdrhistogram-go): Used for latency percentiles.

**Recommended Books:**
- "Get Programming with Go" by Nathan Youngman and Roger Peppé
- "Learning Go" by Jon Bodner
- "100 Go Mistakes and How to Avoid Them" by Teiva Harsanyi

**Blogs:**
- [The Go Blog](https://go.dev/blog/)
- [Dave Cheney's Blog](https://dave.cheney.net/)
- [research!rsc (Russ Cox)](https://research.swtch.com/)

---

## Notes for Non-Go Developers
- Go is a statically typed, compiled language with built-in support for concurrency (goroutines, channels).
- The worker pool pattern is a common way to achieve parallelism in Go.
- Go's standard library and ecosystem make it easy to build high-performance network tools.
- The codebase is modular and readable, making it a good learning resource for Go beginners.

---

## License
MIT 