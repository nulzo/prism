# AI Gateway Benchmarking Suite

This tool performs high-performance load testing on the Model Router API using `vegeta`. It is designed to verify stability, measure latency overhead, and ensure the system handles concurrent streaming connections correctly.

## Overview

The benchmark tool automates a full test environment:
1.  **Mocks Upstream:** Starts a local mock OpenAI-compatible server (port `9091`) to simulate LLM responses without cost.
2.  **Configures Gateway:** Runs your gateway (port `8081`) with a temporary config pointing to the mock.
3.  **Generates Load:** Attacks the gateway with configurable traffic patterns.
4.  **Monitors Resources:** Tracks CPU and Memory usage of the gateway process.

## Usage

Run the benchmark from the project root.

### 1. Standard Throughput Test (Unary)
Use this to measure raw request-response overhead and maximum throughput for non-streaming endpoints.

```bash
# 50 requests/second for 10 seconds
go run cmd/benchmark/bench.go -duration 10s -rate 50
```

### 2. Streaming Load Test (Realistic AI Traffic)
Use this to test connection handling, buffer management, and concurrency. The mock server simulates a slow token stream (approx. 800ms duration per request).

```bash
# 20 concurrent streams starting every second
go run cmd/benchmark/bench.go -duration 10s -rate 20 -stream
```

### Parameters
*   `-duration`: How long to generate traffic (e.g., `5s`, `1m`).
*   `-rate`: Requests per second.
*   `-stream`: If set, sends `stream: true` requests (Server-Sent Events).

## Interpreting Results

### Key Metrics
*   **Success:** Must be **100.00%**. Any drop indicates the gateway is shedding load or crashing.
*   **Throughput:** The effective requests processed per second.
    *   *Note:* For streaming tests, this number naturally lags behind the input `-rate` because it accounts for the time waiting for streams to finish. A throughput of ~160 req/s on a 200 req/s input is normal if streams take 1 second.
*   **99th Percentile (P99) Latency:** The "worst case" experience for users.
    *   **Unary:** Should be close to mock latency (approx 10-20ms).
    *   **Streaming:** Should be consistent with the generation time (approx 1.0 - 1.5s). Spikes here indicate blocking.

### Resource Usage Table
This shows the cost of the load on your hardware.
*   **CPU %:** If this hits 100%, latency will spike.
*   **RSS (MB):** Physical memory usage. Watch for continuous growth (memory leaks) during long runs.

## Targets to Aim For

1.  **Stability:** 100% Success rate at your expected production peak load (e.g., 50 concurrent streams).
2.  **Efficiency:** 
    *   **Idle:** < 20MB RAM.
    *   **Load (100 streams):** < 100MB RAM (Go handles goroutines efficiently).
3.  **Overhead:** The difference between "Mock Latency" (10ms) and "Gateway Latency" should be < 5ms for unary requests.

## Troubleshooting
*   **"bind: address already in use":** Another instance of the benchmark or server is stuck. Kill it or wait a moment.
*   **Success < 100%:** Check `rate_limit` in the generated config or OS file descriptor limits (`ulimit -n`).
