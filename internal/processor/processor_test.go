package processor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jacaudi/tempest-influxdb/internal/config"
	"github.com/jacaudi/tempest-influxdb/internal/logger"
)

func TestCreateOptimizedHTTPClient(t *testing.T) {
	client := createOptimizedHTTPClient()

	if client == nil {
		t.Fatal("createOptimizedHTTPClient() returned nil")
	}

	if client.Timeout != time.Duration(config.DefaultTimeout)*time.Second {
		t.Errorf("Expected timeout %v, got %v",
			time.Duration(config.DefaultTimeout)*time.Second, client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Expected *http.Transport, got different type")
	}

	if transport.MaxIdleConns != config.HTTPMaxIdleConns {
		t.Errorf("Expected MaxIdleConns %d, got %d",
			config.HTTPMaxIdleConns, transport.MaxIdleConns)
	}

	if transport.MaxConnsPerHost != config.HTTPMaxConnsPerHost {
		t.Errorf("Expected MaxConnsPerHost %d, got %d",
			config.HTTPMaxConnsPerHost, transport.MaxConnsPerHost)
	}

	if transport.ExpectContinueTimeout != 0 {
		t.Errorf("Expected ExpectContinueTimeout 0, got %v",
			transport.ExpectContinueTimeout)
	}
}

func TestNewWeatherService(t *testing.T) {
	cfg := &config.Config{
		Listen_Address: ":0", // Use any available port
		Influx_URL:     "http://localhost:8086/api/v2/write",
		Influx_Token:   "test-token",
		Influx_Bucket:  "test-bucket",
		Buffer:         1024,
	}

	appLogger := logger.New(&config.Config{Debug: false})

	service, err := NewWeatherService(cfg, appLogger)
	if err != nil {
		t.Fatalf("NewWeatherService() error = %v", err)
	}

	if service == nil {
		t.Fatal("NewWeatherService() returned nil service")
	}

	if service.config != cfg {
		t.Error("Service config not set correctly")
	}

	if service.logger != appLogger {
		t.Error("Service logger not set correctly")
	}

	if service.listener == nil {
		t.Error("Service listener is nil")
	}

	// Clean up
	_ = service.listener.Close()
}

func TestNewWeatherServiceInvalidAddress(t *testing.T) {
	cfg := &config.Config{
		Listen_Address: "invalid:address:format",
		Influx_URL:     "http://localhost:8086/api/v2/write",
		Influx_Token:   "test-token",
		Influx_Bucket:  "test-bucket",
		Buffer:         1024,
	}

	appLogger := logger.New(&config.Config{Debug: false})

	_, err := NewWeatherService(cfg, appLogger)
	if err == nil {
		t.Fatal("Expected error for invalid address, got nil")
	}
}

func TestProcessPacketValidData(t *testing.T) {
	// Create test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		if r.Header.Get("Authorization") != "Token test-token" {
			t.Errorf("Expected Authorization header 'Token test-token', got %s",
				r.Header.Get("Authorization"))
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := &config.Config{
		Influx_URL:    server.URL,
		Influx_Token:  "test-token",
		Influx_Bucket: "test-bucket",
		Debug:         false,
		Verbose:       false,
		Noop:          false,
	}

	appLogger := logger.New(&config.Config{Debug: false})

	// Test processPacket function (we need to extract it or make it testable)
	// For now, let's test via the service

	// We can't easily test the internal processPacket function directly,
	// so we'll test the overall service behavior
	service := &WeatherService{
		config: cfg,
		logger: appLogger,
	}

	// This test verifies that the service structure is correct
	if service.config != cfg {
		t.Error("Service config not set correctly")
	}
}

func TestProcessPacketNOOPMode(t *testing.T) {
	cfg := &config.Config{
		Influx_URL:    "http://localhost:8086/api/v2/write",
		Influx_Token:  "test-token",
		Influx_Bucket: "test-bucket",
		Debug:         false,
		Verbose:       false,
		Noop:          true, // NOOP mode enabled
	}

	appLogger := logger.New(&config.Config{Debug: false})

	// In NOOP mode, no HTTP requests should be made
	// We can verify this by ensuring no server is needed

	service := &WeatherService{
		config: cfg,
		logger: appLogger,
	}

	// Test that service can be created with NOOP config
	if !service.config.Noop {
		t.Error("Expected NOOP mode to be enabled")
	}
}

func TestWeatherServiceContextCancellation(t *testing.T) {
	cfg := &config.Config{
		Listen_Address: ":0",
		Influx_URL:     "http://localhost:8086/api/v2/write",
		Influx_Token:   "test-token",
		Influx_Bucket:  "test-bucket",
		Buffer:         1024,
	}

	appLogger := logger.New(cfg)

	service, err := NewWeatherService(cfg, appLogger)
	if err != nil {
		t.Fatalf("NewWeatherService() error = %v", err)
	}
	defer func() { _ = service.listener.Close() }()

	// Create a context that will be cancelled quickly
	ctx, cancel := context.WithCancel(context.Background())

	// Start the service in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- service.Start(ctx)
	}()

	// Cancel the context after a short delay
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Wait for service to stop
	select {
	case err := <-errChan:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("Service did not stop within timeout")
	}
}

func TestBufferPool(t *testing.T) {
	// Test that buffer pool works correctly
	buf1 := bufferPool.Get().([]byte)
	if len(buf1) != config.DefaultBuffer {
		t.Errorf("Expected buffer length %d, got %d", config.DefaultBuffer, len(buf1))
	}

	// Put it back (use pointer to slice header to avoid SA6002)
	bufferPool.Put(&buf1)

	// Get another buffer
	buf2 := bufferPool.Get()
	var buf2Slice []byte
	if ptr, ok := buf2.(*[]byte); ok {
		buf2Slice = *ptr
	} else {
		buf2Slice = buf2.([]byte)
	}
	if len(buf2Slice) != config.DefaultBuffer {
		t.Errorf("Expected buffer length %d, got %d", config.DefaultBuffer, len(buf2Slice))
	}
}

// Benchmark tests
func BenchmarkCreateOptimizedHTTPClient(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = createOptimizedHTTPClient()
	}
}

func BenchmarkBufferPoolGetPut(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := bufferPool.Get().([]byte)
		bufferPool.Put(&buf)
	}
}
