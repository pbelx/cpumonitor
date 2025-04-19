package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
    "time"
)

type CPUStats struct {
    Usage    float64   `json:"usage"`
    Timestamp int64    `json:"timestamp"`
}

type CPUUsage struct {
    Total  float64
    Idle   float64
}

func readCPUStat() (CPUUsage, error) {
    file, err := os.Open("/proc/stat")
    if err != nil {
        return CPUUsage{}, err
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "cpu ") {
            fields := strings.Fields(line)[1:] // Skip "cpu" prefix
            
            var values []float64
            for _, field := range fields {
                val, err := strconv.ParseFloat(field, 64)
                if err != nil {
                    return CPUUsage{}, err
                }
                values = append(values, val)
            }
            
            idle := values[3] + values[4]   // idle + iowait
            total := 0.0
            for _, value := range values {
                total += value
            }
            
            return CPUUsage{
                Total: total,
                Idle:  idle,
            }, nil
        }
    }
    
    return CPUUsage{}, fmt.Errorf("CPU stats not found")
}

func calculateCPUPercentage(prev, curr CPUUsage) float64 {
    totalDelta := curr.Total - prev.Total
    idleDelta := curr.Idle - prev.Idle
    
    if totalDelta == 0 {
        return 0.0
    }
    
    return 100.0 * (1.0 - idleDelta/totalDelta)
}

func streamCPUUsage(w http.ResponseWriter, r *http.Request) {
    // Set headers for SSE
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("Access-Control-Allow-Origin", "*")

    // Get initial CPU reading
    prevUsage, err := readCPUStat()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Create channel for detecting client disconnect
    notify := w.(http.CloseNotifier).CloseNotify()
    
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
        return
    }

    // Stream CPU usage every second
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-notify:
            return
        case <-ticker.C:
            currentUsage, err := readCPUStat()
            if err != nil {
                log.Printf("Error reading CPU stats: %v", err)
                continue
            }

            usagePercent := calculateCPUPercentage(prevUsage, currentUsage)
            
            // Create stats object
            stats := CPUStats{
                Usage:     usagePercent,
                Timestamp: time.Now().Unix(),
            }
            
            // Convert to JSON
            jsonData, err := json.Marshal(stats)
            if err != nil {
                log.Printf("Error marshaling JSON: %v", err)
                continue
            }
            
            // Send the CPU usage as an SSE event
            fmt.Fprintf(w, "data: %s\n\n", jsonData)
            flusher.Flush()
            
            prevUsage = currentUsage
        }
    }
}

func main() {
    // Enable CORS
    corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Access-Control-Allow-Origin", "*")
            w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
            
            if r.Method == "OPTIONS" {
                w.WriteHeader(http.StatusOK)
                return
            }
            
            next(w, r)
        }
    }

    // Handle SSE endpoint
    http.HandleFunc("/events", corsMiddleware(streamCPUUsage))

    // Start server
    log.Println("Server starting on http://localhost:8080")
    if err := http.ListenAndServe(":8080", nil); err != nil {
        log.Fatal(err)
    }
}
