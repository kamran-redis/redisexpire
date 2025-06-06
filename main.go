package main

import (
	"fmt"
	"os"
)

func main() {
	cfg, err := ParseConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config: %+v\n", cfg)
	fmt.Println("Starting benchmark...")

	runWorkerPool(cfg.Concurrency, cfg.Requests, cfg)

	fmt.Println("Benchmark complete.")
}
