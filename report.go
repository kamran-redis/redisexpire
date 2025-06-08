// Package main: report.go
// Contains all metrics reporting and formatting logic for the Redis benchmarking tool.
package main

import (
	"fmt"
	"time"

	hdr "github.com/HdrHistogram/hdrhistogram-go"
)

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
