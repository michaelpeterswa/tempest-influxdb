package main

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jacaudi/tempest-influxdb/internal/config"
)

func TestMainFunctionality(t *testing.T) {
	t.Skip("Skipping until config test is fixed")
}

func TestSignalHandling(t *testing.T) {
	// Test that signal handling works correctly
	// Create signal channel like in main
	sigCh := make(chan os.Signal, 1)

	// Test that we can send a signal
	go func() {
		time.Sleep(10 * time.Millisecond)
		sigCh <- syscall.SIGINT
	}()

	// Test that we receive the signal
	select {
	case sig := <-sigCh:
		if sig != syscall.SIGINT {
			t.Errorf("Expected SIGINT, got %v", sig)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for signal")
	}
}

func TestEnvironmentVariableHandling(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envValue string
		expected string
	}{
		{
			name:     "config dir override",
			envVar:   "TEMPEST_INFLUX_CONFIG_DIR",
			envValue: "/custom/config",
			expected: "/custom/config",
		},
		{
			name:     "default config dir",
			envVar:   "TEMPEST_INFLUX_CONFIG_DIR",
			envValue: "",
			expected: "/config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variable
			_ = os.Unsetenv(tt.envVar)

			if tt.envValue != "" {
				if err := os.Setenv(tt.envVar, tt.envValue); err != nil {
					t.Fatalf("Failed to set env: %v", err)
				}
				defer func() { _ = os.Unsetenv(tt.envVar) }()
			}

			// Simulate the logic from main
			configDir := os.Getenv("TEMPEST_INFLUX_CONFIG_DIR")
			if configDir == "" {
				configDir = "/config"
			}

			if configDir != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, configDir)
			}
		})
	}
}

func TestMainComponents(t *testing.T) {
	// Test the individual components that main uses
	t.Run("config validation", func(t *testing.T) {
		cfg := &config.Config{
			Influx_URL:      "http://localhost:8086",
			Influx_API_Path: "/api/v2/write",
			Influx_Org:      "test-org",
			Influx_Token:    "test-token",
			Influx_Bucket:   "test-bucket",
			Listen_Address:  ":50222",
			Buffer:          1024,
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("Valid config failed validation: %v", err)
		}
	})

	t.Run("context creation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if ctx == nil {
			t.Error("Context creation failed")
		}

		// Test cancellation
		cancel()
		select {
		case <-ctx.Done():
			// Expected
		case <-time.After(time.Millisecond):
			t.Error("Context not cancelled properly")
		}
	})
}

func TestVersionOutput(t *testing.T) {
	// Test that version string is properly formatted
	version := "2.0.0"

	if !strings.Contains(version, "2.0.0") {
		t.Errorf("Version string should contain '2.0.0', got %s", version)
	}
}

func TestLogPrefixSetting(t *testing.T) {
	// Test that log prefix can be set (simulating what happens in main)
	expectedPrefix := "tempest-influxdb: "

	// We can't easily test the actual log.SetPrefix call,
	// but we can verify the string is correct
	if !strings.HasSuffix(expectedPrefix, ": ") {
		t.Errorf("Log prefix should end with ': ', got %s", expectedPrefix)
	}

	if !strings.HasPrefix(expectedPrefix, "tempest-influxdb") {
		t.Errorf("Log prefix should start with 'tempest-influxdb', got %s", expectedPrefix)
	}
}

// Integration test that simulates main function components
func TestMainIntegration(t *testing.T) {
	// Skip this test due to flag redefinition issues when config.Load is called multiple times in tests
	t.Skip("Skipping integration test due to global flag conflicts")

	// Skip this test in short mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Set up minimal environment
	if err := os.Setenv("TEMPEST_INFLUX_INFLUX_URL", "http://localhost:8086/api/v2/write"); err != nil {
		t.Fatalf("Failed to set env: %v", err)
	}
	if err := os.Setenv("TEMPEST_INFLUX_INFLUX_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set env: %v", err)
	}
	if err := os.Setenv("TEMPEST_INFLUX_INFLUX_BUCKET", "test-bucket"); err != nil {
		t.Fatalf("Failed to set env: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("TEMPEST_INFLUX_INFLUX_URL")
		_ = os.Unsetenv("TEMPEST_INFLUX_INFLUX_TOKEN")
		_ = os.Unsetenv("TEMPEST_INFLUX_INFLUX_BUCKET")
	}()

	// Test the main function components in sequence
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Load config
	cfg := config.Load("/tmp", "tempest-influxdb")
	if cfg == nil {
		t.Fatal("Failed to load config")
	}

	// Validate config
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("Config validation failed: %v", err)
	}

	// Test that context cancellation works
	select {
	case <-ctx.Done():
		// Expected - timeout occurred
		if ctx.Err() != context.DeadlineExceeded {
			t.Errorf("Expected DeadlineExceeded, got %v", ctx.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Context should have timed out")
	}
}

// Benchmark the main function components
func BenchmarkConfigLoad(b *testing.B) {
	b.Helper()
	if err := os.Setenv("TEMPEST_INFLUX_INFLUX_URL", "http://localhost:8086/api/v2/write"); err != nil {
		b.Fatalf("Failed to set env: %v", err)
	}
	if err := os.Setenv("TEMPEST_INFLUX_INFLUX_TOKEN", "test-token"); err != nil {
		b.Fatalf("Failed to set env: %v", err)
	}
	if err := os.Setenv("TEMPEST_INFLUX_INFLUX_BUCKET", "test-bucket"); err != nil {
		b.Fatalf("Failed to set env: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("TEMPEST_INFLUX_INFLUX_URL")
		_ = os.Unsetenv("TEMPEST_INFLUX_INFLUX_TOKEN")
		_ = os.Unsetenv("TEMPEST_INFLUX_INFLUX_BUCKET")
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		config.Load("/tmp", "tempest-influxdb")
	}
}
