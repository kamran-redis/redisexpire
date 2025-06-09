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
| `-expiry-at`   | (empty)     | Absolute expiry for all keys, as a duration from start (e.g., 5m). Overrides -expiry. |
| `-data-type`   | string      | Data type to write: string, hash, or empty        |

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

**Set all keys to expire exactly 5 minutes from the start:**
```sh
./redisbench -expiry-at=5m
```

**Run a dummy benchmark to measure raw framework overhead (no Redis commands):**
```sh
./redisbench -data-type=empty -concurrency=100 -requests=10000
```

**Example output**
```
./redisbench --concurrency=50 --duration=10s  -value-size=5  --data-type=string --expiry=1 --rps=190000
Config: &{Host:localhost Port:6379 Password: Concurrency:50 Requests:10000 RPS:190000 Duration:10s ValueSize:5 Expiry:1 DataType:string ExpiryAtRaw: ExpiryAt:0001-01-01 00:00:00 +0000 UTC PipelineSize:1}
Starting benchmark...
Running for duration: 10s
Time(s)  Requests   Errors   Min        Max        Mean       P50        P99        P99.9      P99.99     Throughput
1.0      190009     0        9µs        22.639ms   160µs      122µs      584µs      1.406ms    21.487ms   190009.00
2.0      378073     0        9µs        16.159ms   177µs      139µs      558µs      5.295ms    16.103ms   188064.00
3.0      568712     0        9µs        26.751ms   231µs      164µs      1.295ms    13.135ms   26.703ms   190639.00
4.0      760019     0        9µs        26.655ms   212µs      153µs      570µs      18.591ms   26.575ms   191307.00
5.0      949809     0        9µs        27.311ms   228µs      168µs      1.214ms    13.615ms   26.719ms   189790.00
6.0      1123836    0        7µs        26.815ms   281µs      193µs      1.371ms    17.423ms   26.703ms   174027.00
7.0      1317190    0        10µs       26.655ms   247µs      180µs      1.119ms    21.423ms   26.607ms   193354.00
8.0      1517246    0        9µs        26.511ms   215µs      160µs      1.255ms    20.943ms   22.223ms   200056.00
9.0      1708603    0        8µs        27.391ms   224µs      163µs      1.267ms    14.807ms   26.847ms   191357.00
10.0     1898983    0        10µs       31.791ms   219µs      154µs      1.178ms    14.015ms   26.911ms   190380.00
Time(s)  Requests   Errors   Min        Max        Mean       P50        P99        P99.9      P99.99     Throughput
TOTAL    1900058    0        7µs        31.791ms   219µs      160µs      1.182ms    13.367ms   26.655ms   189935.57
```

```
./redisbench --concurrency=50 --requests=2000000 -value-size=5  --data-type=string --expiry=1
./redisbench --concurrency=50 --requests=2000000 -value-size=5  --data-type=hash 
./redisbench --concurrency=50 --requests=2000000 -value-size=5  --data-type=hash --expiry=1
./redisbench --concurrency=50 --requests=2000000 -value-size=5  --data-type=hash --expiry=120
./redisbench --concurrency=50 --requests=5000000 -value-size=5  --data-type=hash -expiry-at=120s
```

```
FT.CREATE idx ON HASH PREFIX 1 bench: SCHEMA field1 TAG
CONFIG SET search-timeout  2000
FT. idx "*" limit 0 1
FT.AGGREGATE idx * LOAD 1 field1 limit 0 1
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
- The `empty` data type can be used to measure the raw performance of the benchmarking framework itself, without sending any commands to Redis (useful for baseline comparison).

### 6. Simplicity and Extensibility
- The codebase is kept simple and modular, making it easy to extend (e.g., to add new Redis commands or metrics).
- Error handling and input validation are included to prevent misconfiguration and provide clear feedback.

## Notes on Expiry

- If you use `-expiry-at`, all keys will be set to expire at the same absolute time, calculated as the specified duration from the start of the benchmark.
- If both `-expiry` and `-expiry-at` are set, `-expiry-at` takes precedence.
- The minimum expiry set is always 1 second, even if the calculated expiry is zero or negative (e.g., if the benchmark runs longer than the expiry window). 