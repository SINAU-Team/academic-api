package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// Config holds the proxy configuration
type Config struct {
	Server struct {
		Port         int      `yaml:"port"`
		ReadTimeout  int      `yaml:"read_timeout"`
		WriteTimeout int      `yaml:"write_timeout"`
		AllowedIPs   []string `yaml:"allowed_ips"` // Empty = allow all
	} `yaml:"server"`
	Dapodik struct {
		BaseURL string `yaml:"base_url"` // Default Dapodik URL if not specified in request
	} `yaml:"dapodik"`
	Logging struct {
		Level  string `yaml:"level"`
		Pretty bool   `yaml:"pretty"`
	} `yaml:"logging"`
	Security struct {
		APIKey string `yaml:"api_key"` // Optional: require API key to access proxy
	} `yaml:"security"`
}

var config Config

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to config file")
	port := flag.Int("port", 0, "Override port from config")
	flag.Parse()

	// Load configuration
	if err := loadConfig(*configPath); err != nil {
		fmt.Printf("Warning: Could not load config file: %v\n", err)
		fmt.Println("Using default configuration...")
		setDefaultConfig()
	}

	// Override port if specified
	if *port > 0 {
		config.Server.Port = *port
	}

	// Setup logging
	setupLogging()

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleProxy)
	mux.HandleFunc("/health", handleHealth)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Server.Port),
		Handler:      loggingMiddleware(corsMiddleware(mux)),
		ReadTimeout:  time.Duration(config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(config.Server.WriteTimeout) * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Info().Msg("Shutting down proxy server...")
		server.Close()
	}()

	// Start server
	log.Info().
		Int("port", config.Server.Port).
		Str("dapodik_base_url", config.Dapodik.BaseURL).
		Msg("Starting Dapodik Proxy Server")

	authStatus := "Disabled (Insecure)"
	if config.Security.APIKey != "" {
		authStatus = "Enabled (Secure)"
	}

	fmt.Printf("\n")
	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║           DAPODIK PROXY SERVER                               ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Status  : Running                                           ║\n")
	fmt.Printf("║  Port    : %-49d ║\n", config.Server.Port)
	fmt.Printf("║  Security: %-49s ║\n", authStatus)
	fmt.Printf("║  Health  : http://localhost:%-33s ║\n", fmt.Sprintf("%d/health", config.Server.Port))
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Usage Example:                                              ║\n")
	fmt.Printf("║  GET /proxy?target=http://IP:PORT/WebService/getSekolah      ║\n")
	fmt.Printf("║      &npsn=20275048                                          ║\n")
	fmt.Printf("║                                                              ║\n")
	fmt.Printf("║  Headers: Authorization: Bearer <dapodik_token>              ║\n")
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")
	fmt.Printf("\n")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}

func loadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &config)
}

func setDefaultConfig() {
	config.Server.Port = 8888
	config.Server.ReadTimeout = 60
	config.Server.WriteTimeout = 120
	config.Dapodik.BaseURL = ""
	config.Logging.Level = "info"
	config.Logging.Pretty = true
}

func setupLogging() {
	// Set log level
	level, err := zerolog.ParseLevel(config.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Pretty print for console
	if config.Logging.Pretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05",
		})
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create response wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("query", r.URL.RawQuery).
			Int("status", rw.statusCode).
			Dur("duration", time.Since(start)).
			Str("remote_addr", r.RemoteAddr).
			Msg("Request processed")
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Proxy-Target, X-API-Key")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"dapodik-proxy"}`))
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	// Check API key if configured
	if config.Security.APIKey != "" {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != config.Security.APIKey {
			http.Error(w, `{"error":"Unauthorized - Invalid API Key"}`, http.StatusUnauthorized)
			return
		}
	}

	// Check allowed IPs if configured
	if len(config.Server.AllowedIPs) > 0 {
		clientIP := getClientIP(r)
		allowed := false
		for _, ip := range config.Server.AllowedIPs {
			if ip == clientIP || ip == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			log.Warn().Str("client_ip", clientIP).Msg("IP not in allowed list")
			http.Error(w, `{"error":"Forbidden - IP not allowed"}`, http.StatusForbidden)
			return
		}
	}

	// Get target URL from query parameter or header
	targetURL := r.URL.Query().Get("target")
	if targetURL == "" {
		targetURL = r.Header.Get("X-Proxy-Target")
	}

	// If still empty, try to construct from path
	if targetURL == "" && config.Dapodik.BaseURL != "" {
		// Use path after /proxy/ as the endpoint
		path := strings.TrimPrefix(r.URL.Path, "/proxy/")
		path = strings.TrimPrefix(path, "/")
		if path != "" {
			targetURL = config.Dapodik.BaseURL + "/" + path
			// Append query string
			if r.URL.RawQuery != "" {
				targetURL += "?" + r.URL.RawQuery
			}
		}
	}

	if targetURL == "" {
		http.Error(w, `{"error":"Missing target URL. Use ?target=URL or X-Proxy-Target header"}`, http.StatusBadRequest)
		return
	}

	log.Debug().Str("target", targetURL).Msg("Proxying request")

	// Create proxy request
	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create proxy request")
		http.Error(w, fmt.Sprintf(`{"error":"Failed to create request: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Copy headers from original request
	for key, values := range r.Header {
		// Skip hop-by-hop headers
		if isHopByHopHeader(key) {
			continue
		}
		// Skip proxy-specific headers
		if key == "X-Proxy-Target" || key == "X-API-Key" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Ensure we have Accept header
	if proxyReq.Header.Get("Accept") == "" {
		proxyReq.Header.Set("Accept", "application/json")
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(config.Server.WriteTimeout) * time.Second,
	}

	// Execute request
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Error().Err(err).Str("target", targetURL).Msg("Proxy request failed")
		http.Error(w, fmt.Sprintf(`{"error":"Proxy request failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read response body")
		http.Error(w, fmt.Sprintf(`{"error":"Failed to read response: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Copy response headers
	for key, values := range resp.Header {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set CORS headers (override if present)
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Write response
	w.WriteHeader(resp.StatusCode)
	w.Write(body)

	log.Debug().
		Int("status", resp.StatusCode).
		Int("body_size", len(body)).
		Msg("Proxy response sent")
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
		ip = ip[:colonIdx]
	}
	return ip
}

func isHopByHopHeader(header string) bool {
	hopByHopHeaders := map[string]bool{
		"Connection":          true,
		"Keep-Alive":          true,
		"Proxy-Authenticate":  true,
		"Proxy-Authorization": true,
		"Te":                  true,
		"Trailers":            true,
		"Transfer-Encoding":   true,
		"Upgrade":             true,
	}
	return hopByHopHeaders[header]
}
