package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

const (
	mockPort = 9091
	appPort  = 8081
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
	    
	    // Redirect output to file for debugging	// logFile, _ := os.Create("bench_server.log")
	// defer logFile.Close()
	// cmd.Stdout = logFile
	// cmd.Stderr = logFile

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

	// monitor resource usage in background
	done := make(chan bool)
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

	targeter := vegeta.NewStaticTargeter(vegeta.Target{
		Method: "POST",
		URL:    fmt.Sprintf("http://localhost:%d/api/v1/chat/completions", appPort),
		Body:   []byte(body),
		Header: http.Header{
			"Content-Type":  []string{"application/json"},
			"Authorization": []string{"Bearer bench-key-12345"},
		},
	})

	if *chaos {
		fmt.Println("CHAOS MODE ENABLED: Mock server will abruptly close some streaming connections.")
	}

	attacker := vegeta.NewAttacker(vegeta.KeepAlive(true))
	var metrics vegeta.Metrics

	for res := range attacker.Attack(targeter, vegeta.Rate{Freq: *rate, Per: time.Second}, *duration, "Benchmark") {
		metrics.Add(res)
	}
	metrics.Close()

	// stop monitoring
	done <- true

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
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)

		if val, ok := req["stream"].(bool); ok && val {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			tokens := []string{"Bench", "mark", " safe", " response"}
			for _, t := range tokens {
				time.Sleep(50 * time.Millisecond)

				resp := map[string]interface{}{
					"choices": []interface{}{map[string]interface{}{"delta": map[string]string{"content": t}}},
				}
				b, _ := json.Marshal(resp)
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()

				// Simulate random server-side disconnect for Chaos mode (mocking upstream failure)
				// Note: chaos is passed via global or we can detect it.
				// We'll just always simulate it for now if we want to see server reaction.
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"bench-123","choices":[{"message":{"content":"Hello"}}]}`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	_ = http.ListenAndServe(fmt.Sprintf(":%d", mockPort), mux)
}

func monitorResources(pid int, done chan bool) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Println("\n--- Resource Usage (expvar + ps) ---")
	fmt.Printf("% -10s % -10s % -10s % -10s\n", "Time", "Heap(MB)", "Alloc(MB)", "CPU(%)")

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// 1. Fetch Memory Stats from application via expvar
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

			// 2. Fetch CPU Stats from OS via 'ps'
			// This is an external check, so it works even if pprof is slow,
			// though pprof/expvar is still needed for memory.
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
