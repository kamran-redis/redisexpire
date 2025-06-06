# RedisExpire Benchmark Tool

## Overview

**RedisExpire** is a high-performance benchmarking tool for Redis, written in Go. It allows you to stress-test your Redis server by sending a large number of SET or HSET commands (with optional expiry) using multiple concurrent workers. The tool reports real-time and summary statistics about request latency, throughput, and error rates.

## Features
- Configurable concurrency (number of workers)
- Configurable total requests or test duration
- Optional requests-per-second (RPS) rate limiting
- Support for string and hash data types
- Optional key expiry
- Real-time and summary metrics (latency, throughput, percentiles, errors)

## Requirements
- Go 1.18 or newer
- A running Redis server

## Building

Clone the repository and build the binary:

```sh
git clone <repo-url>
cd redisexpire
go build -o redisbench
```

This will produce an executable named `redisbench`.

## Usage

Run the benchmark tool with desired options:

```sh
./redisbench [flags]
```

### Command-Line Flags

| Flag           | Default     | Description                                      |
| -------------- | ----------- | ------------------------------------------------ |
| `-host`        | localhost   | Redis server host                                |
| `-port`        | 6379        | Redis server port                                |
| `-password`    | (empty)     | Redis server password                            |
| `-concurrency` | 50          | Number of concurrent workers                     |
| `-requests`    | 10000       | Total number of requests (ignored if duration)   |
| `-duration`    | 0           | How long to run the test (e.g., 10s, 1m)         |
| `-rps`         | 0           | Requests per second (0 = unlimited)              |
| `-value-size`  | 128         | Size of value in bytes for SET commands          |
| `-expiry`      | 0           | Expiry in seconds for SET keys (0 = no expiry)   |
| `-data-type`   | string      | Data type to write: string or hash               |

### Examples

**Run 10,000 requests with 100 workers:**
```sh
./redisbench -concurrency=100 -requests=10000
```

**Run for 30 seconds at 500 RPS:**
```sh
./redisbench -duration=30s -rps=500
```

**Use hash data type with 256-byte values and 60s expiry:**
```sh
./redisbench -data-type=hash -value-size=256 -expiry=60
```

## Output

The tool prints real-time statistics every second, including:
- Total requests sent
- Errors encountered
- Latency (min, max, mean, percentiles)
- Throughput (requests per second)

A final summary is printed at the end of the benchmark.

## License

MIT 

## Design Decisions

### 1. Worker Pool Model
- The tool uses a worker pool pattern to simulate high concurrency and parallelism, reflecting real-world usage where many clients interact with Redis simultaneously.
- Each worker maintains its own Redis client connection for efficiency and to avoid contention.

### 2. Configurability
- All key parameters (concurrency, requests, duration, RPS, value size, expiry, data type) are exposed as command-line flags, making the tool flexible for different benchmarking scenarios.
- The tool supports both a fixed number of requests and a fixed duration, allowing for different types of load tests.

### 3. Rate Limiting (RPS)
- RPS is implemented using Go's `golang.org/x/time/rate` package, which provides a robust token bucket rate limiter.
- The limiter is used to throttle job dispatching, ensuring the desired request rate is not exceeded.
- Burst size is set to 1 to minimize burstiness and keep request intervals as even as possible.

### 4. Real-Time Metrics
- A dedicated goroutine aggregates and prints real-time metrics (latency, throughput, errors) every second, providing immediate feedback during tests.
- Latency percentiles and throughput are calculated using the HdrHistogram library for accuracy and performance.

### 5. Data Type and Expiry Support
- The tool supports both string and hash data types, covering common Redis use cases.
- Optional expiry for keys allows benchmarking of Redis' key expiration mechanism.

### 6. Simplicity and Extensibility
- The codebase is kept simple and modular, making it easy to extend (e.g., to add new Redis commands or metrics).
- Error handling and input validation are included to prevent misconfiguration and provide clear feedback. 