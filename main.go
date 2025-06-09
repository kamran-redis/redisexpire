package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	cfg, err := ParseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config: %+v\n", cfg)

	if cfg.ExpiryAtRaw != "" {
		fmt.Printf("All keys will expire at: %s (RFC3339)\n", cfg.ExpiryAt.Format(time.RFC3339))
	}

	fmt.Println("Starting benchmark...")

	//flag.IntVar(&cfg.Pipeline, "pipeline", 1, "Number of commands per pipeline batch (for pipelining)")
	//flag.Parse()

	runWorkerPool(cfg.Concurrency, cfg.Requests, cfg)

	fmt.Println("Benchmark complete.")
}
