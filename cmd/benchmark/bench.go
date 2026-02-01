package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	mockPort = 9091
	appPort  = 8081
)

var (
	streamChunk1 = []byte(`data: {"choices":[{"delta":{"content":"Bench"}}]}\n\n`)
	streamChunk2 = []byte(`data: {"choices":[{"delta":{"content":"mark"}}]}\n\n`)
	streamChunk3 = []byte(`data: {"choices":[{"delta":{"content":" safe"}}]}\n\n`)
	streamChunk4 = []byte(`data: {"choices":[{"delta":{"content":" response"}}]}\n\n`)
	streamDone   = []byte(`data: [DONE]\n\n`)
	unaryResp    = []byte(`{"id":"bench-123","choices":[{"message":{"content":"Hello"}}]}`)
)

func main() {
	duration := flag.Duration("duration", 10*time.Second, "Duration of the test")
	rate := flag.Int("rate", 50, "Requests per second")
	stream := flag.Bool("stream", false, "Use streaming requests")
	chaos := flag.Bool("chaos", false, "Simulate random client disconnections")
	flag.Parse()

	// start mock server
	go startMockServer()

	// build and start application
	fmt.Println("Building application...")
	buildCmd := exec.Command("go", "build", "-o", "bin/server", "./cmd/server")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		log.Fatalf("Failed to build app: %v", err)
	}

	// Create a temporary config file for the benchmark
	configFile := "bench_config_safe.yaml"
	if err := os.WriteFile(configFile, []byte(benchConfig), 0644); err != nil {
		log.Fatalf("Failed to write config: %v", err)
	}
	defer os.Remove(configFile)

	fmt.Println("Starting application...")
	cmd := exec.Command("./bin/server")

	// FORCE the app to use our config file and specific port
	cmd.Env = append(os.Environ(), fmt.Sprintf("CONFIG_FILE=%s", configFile))
	cmd.Env = append(cmd.Env, fmt.Sprintf("SERVER_PORT=%d", appPort))
	cmd.Env = append(cmd.Env, "LOG_LEVEL=error")

	// Redirect output to file for debugging
	logFile, _ := os.Create("bench_server.log")
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start app: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()

	// Wait for app to be ready
	waitForApp(fmt.Sprintf("http://localhost:%d/health", appPort))

	// Signal channel to stop background tasks (monitor, chaos monkey)
	done := make(chan struct{})

	// monitor resource usage in background
	go func() {
		// Wait for pprof/expvar to initialize
		time.Sleep(2 * time.Second)
		monitorResources(cmd.Process.Pid, done)
	}()

	// run vegeta attack
	mode := "Unary"
	if *stream {
		mode = "Streaming"
	}
	fmt.Printf("Running %s benchmark: %s duration, %d req/s\n", mode, *duration, *rate)

	// Using an existing model ID from the app's default config to ensure resolution works
	body := `{"model": "openai/gpt-3.5-turbo", "messages": [{"role": "user", "content": "Hello"}]}`
	if *stream {
		body = `{"model": "openai/gpt-3.5-turbo", "stream": true, "messages": [{"role": "user", "content": "Hello"}]}`
	}

	// "Gold Standard" Targeter: Dynamically inject timestamp into headers
	targeter := func(t *vegeta.Target) error {
		t.Method = "POST"
		t.URL = fmt.Sprintf("http://localhost:%d/api/v1/chat/completions", appPort)
		t.Body = []byte(body)
		t.Header = http.Header{
			"Content-Type":      []string{"application/json"},
			"Authorization":     []string{"Bearer bench-key-12345"},
			"X-Benchmark-Start": []string{strconv.FormatInt(time.Now().UnixNano(), 10)},
		}
		return nil
	}

	if *chaos {
		fmt.Println("CHAOS MODE ENABLED: Starting Chaos Monkey sidecar...")
		chaosConcurrency := *rate / 10
		if chaosConcurrency < 5 {
			chaosConcurrency = 5
		}
		if chaosConcurrency > 50 {
			chaosConcurrency = 50
		}
		go startChaosMonkey(fmt.Sprintf("http://localhost:%d/api/v1/chat/completions", appPort), chaosConcurrency, done)
	}

	attacker := vegeta.NewAttacker(vegeta.KeepAlive(true))
	var metrics vegeta.Metrics

	for res := range attacker.Attack(targeter, vegeta.Rate{Freq: *rate, Per: time.Second}, *duration, "Benchmark") {
		metrics.Add(res)
	}
	metrics.Close()

	// stop monitoring and chaos monkey
	close(done)

	fmt.Println("--------------------------------------------------")
	fmt.Println("99th percentile: ", metrics.Latencies.P99)
	fmt.Println("Mean:            ", metrics.Latencies.Mean)
	fmt.Println("Max:             ", metrics.Latencies.Max)
	fmt.Printf("Success:         %.2f%%\n", metrics.Success*100)
	fmt.Printf("Throughput:      %.2f req/s\n", metrics.Throughput)
	fmt.Println("--------------------------------------------------")

	if len(metrics.Errors) > 0 {
		fmt.Println("Error Set (first 5 unique):")

		uniqueErrors := make(map[string]bool)
		count := 0
		for _, msg := range metrics.Errors {
			if !uniqueErrors[msg] && count < 5 {
				fmt.Println(msg)

				uniqueErrors[msg] = true
				count++
			}
		}
	}

	// Cleanup
	os.Remove("bench.db")
}

func startChaosMonkey(url string, concurrency int, done chan struct{}) {
	fmt.Printf("Starting Chaos Monkey with %d concurrent disrupters (random disconnects 1-200ms)\n", concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			client := &http.Client{
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 100,
					DisableKeepAlives:   false,
				},
			}

			payload := `{"model": "openai/gpt-3.5-turbo", "stream": true, "messages": [{"role": "user", "content": "Chaos Request"}]}`

			for {
				select {
				case <-done:
					return
				default:
					// Randomly disconnect between 1ms and 200ms
					timeout := time.Duration(rand.Intn(200)+1) * time.Millisecond

					ctx, cancel := context.WithTimeout(context.Background(), timeout)
					req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(payload))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Authorization", "Bearer bench-key-12345")

					resp, err := client.Do(req)
					if err == nil {
						resp.Body.Close()
					}
					cancel()

					// Sleep briefly to control request rate per goroutine
					time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
				}
			}
		}()
	}
}

func startMockServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "gpt-3.5-turbo", "object": "model", "created": 1687882411, "owned_by": "openai"}
			]
		}`))
	})

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		startStr := r.Header.Get("X-Benchmark-Start")
		if startStr != "" {
			start, _ := strconv.ParseInt(startStr, 10, 64)
			latency := time.Now().UnixNano() - start
			// Sample 1% of requests to avoid console spam
			if rand.Intn(100) == 0 {
				fmt.Printf("DEBUG: Proxy Overhead: %v\n", time.Duration(latency))
			}
		}

		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if val, ok := req["stream"].(bool); ok && val {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)

			chunks := [][]byte{streamChunk1, streamChunk2, streamChunk3, streamChunk4}
			for _, chunk := range chunks {
				time.Sleep(50 * time.Millisecond)
				w.Write(chunk)
				flusher.Flush()
			}
			w.Write(streamDone)
			flusher.Flush()
			return
		}

		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write(unaryResp)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	_ = http.ListenAndServe(fmt.Sprintf(":%d", mockPort), mux)
}

func monitorResources(pid int, done chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Println("\n--- Resource Usage (expvar + ps) ---")
	fmt.Printf("% -10s % -10s % -10s % -10s\n", "Time", "Heap(MB)", "Alloc(MB)", "CPU(%)")

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			resp, err := http.Get("http://127.0.0.1:6060/debug/vars")
			if err != nil {
				fmt.Printf("DEBUG: monitorResources failed to reach expvar: %v\n", err)
				continue
			}

			var vars struct {
				MemStats struct {
					HeapInuse uint64 `json:"HeapInuse"`
					Alloc     uint64 `json:"Alloc"`
				} `json:"memstats"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&vars); err != nil {
				resp.Body.Close()
				continue
			}
			resp.Body.Close()

			cpu := 0.0
			out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu").Output()
			if err == nil {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				if len(lines) >= 2 {
					val, _ := strconv.ParseFloat(strings.TrimSpace(lines[1]), 64)
					cpu = val
				}
			}

			fmt.Printf("% -10s % -10.2f % -10.2f % -10.2f\n",
				time.Now().Format("15:04:05"),
				float64(vars.MemStats.HeapInuse)/1024/1024,
				float64(vars.MemStats.Alloc)/1024/1024,
				cpu,
			)
		}
	}
}

func waitForApp(url string) {
	for i := 0; i < 20; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Fatal("App timed out")
}

var benchConfig = fmt.Sprintf(`
server:
  port: %d
  env: development
  auth_enabled: false
  api_keys: ["bench-key-12345"]
rate_limit:
  requests_per_second: 100000
  burst: 100000
log:
  level: "error"
database:
  path: "bench.db"
providers:
  - id: openai
    type: openai
    name: OpenAI
    api_key: "mock-key"
    base_url: "http://localhost:%d/v1"
    enabled: true
    requires_auth: true
`, appPort, mockPort)
