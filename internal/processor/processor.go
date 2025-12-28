package processor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jacaudi/tempest-influxdb/internal/config"
	"github.com/jacaudi/tempest-influxdb/internal/influx"
	"github.com/jacaudi/tempest-influxdb/internal/logger"
	"github.com/jacaudi/tempest-influxdb/internal/tempest"
	"github.com/samber/lo"
)

// Buffer pool for reusing byte buffers to reduce GC pressure
var bufferPool = sync.Pool{
	New: func() any {
		return make([]byte, config.DefaultBuffer)
	},
}

// createOptimizedHTTPClient creates an HTTP client with optimized settings
func createOptimizedHTTPClient() *http.Client {
	transport := &http.Transport{
		MaxIdleConns:          config.HTTPMaxIdleConns,
		MaxConnsPerHost:       config.HTTPMaxConnsPerHost,
		IdleConnTimeout:       config.HTTPIdleConnTimeout * time.Second,
		ExpectContinueTimeout: 0, // Skip expect-continue for better latency
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   time.Duration(config.DefaultTimeout) * time.Second,
	}
}

// processPacket processes a weather data packet
func processPacket(ctx context.Context, cfg *config.Config, logger *logger.AppLogger, influxURL *url.URL, addr *net.UDPAddr, b []byte, n int) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Recovered from panic in packet processing",
				"panic", r.(string),
				"remote_addr", addr.String())
		}
	}()

	// Use Lo library for safer error handling
	m, ok := lo.TryOr(func() (*influx.Data, error) {
		return tempest.Parse(cfg, addr, b, n)
	}, nil)

	if !ok || m == nil {
		return
	}

	if m.Timestamp == 0 {
		return
	}

	if cfg.Debug {
		logger.Debug("Processing InfluxData",
			"measurement", m.Name,
			"timestamp", m.Timestamp,
			"bucket", m.Bucket)
	}

	line := m.Marshal()
	if cfg.Verbose {
		logger.Info("Posting data to InfluxDB",
			"data", line,
			"url", influxURL.String())
	}

	if m.Bucket != "" {
		// Set query arguments, preserving existing parameters like org
		query := influxURL.Query()
		query.Set("bucket", m.Bucket)
		influxURL.RawQuery = query.Encode()
	}

	// Create HTTP request with context
	request, err := http.NewRequestWithContext(ctx, "POST", influxURL.String(), strings.NewReader(line))
	if err != nil {
		logger.Error("Failed to create HTTP request",
			"error", err.Error(),
			"url", influxURL.String())
		return
	}
	request.Header.Set("Authorization", "Token "+cfg.Influx_Token)
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	request.Header.Set("Accept", "application/json")

	if cfg.Noop {
		logger.Info("NOOP mode - not posting to InfluxDB",
			"url", influxURL.String())
		return
	}

	// Optimized HTTP client with proper transport configuration
	client := createOptimizedHTTPClient()

	// Use Lo library for safer HTTP request handling
	resp, ok := lo.TryOr(func() (*http.Response, error) {
		return client.Do(request)
	}, nil)

	if !ok || resp == nil {
		logger.Error("Failed to post data to InfluxDB",
			"influx_url", cfg.Influx_URL)
		return
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		logger.Error("InfluxDB returned error status",
			"status", resp.Status,
			"status_code", resp.StatusCode)
	} else if cfg.Verbose {
		logger.Info("Successfully posted data to InfluxDB",
			"status", resp.Status,
			"status_code", resp.StatusCode)
	}
}

// WeatherService manages the weather data collection service
type WeatherService struct {
	config   *config.Config
	logger   *logger.AppLogger
	listener net.PacketConn
}

// NewWeatherService creates a new WeatherService
func NewWeatherService(cfg *config.Config, appLogger *logger.AppLogger) (*WeatherService, error) {
	// Create UDP listener
	sourceAddr, err := net.ResolveUDPAddr("udp", cfg.Listen_Address)
	if err != nil {
		return nil, err
	}

	sourceConn, err := net.ListenUDP("udp", sourceAddr)
	if err != nil {
		return nil, err
	}

	return &WeatherService{
		config:   cfg,
		logger:   appLogger,
		listener: sourceConn,
	}, nil
}

// Start starts the weather service
func (ws *WeatherService) Start(ctx context.Context) error {
	ws.logger.Info("Weather service started")

	defer func() { _ = ws.listener.Close() }()

	// Parse Influx URL and append API path
	influxURL, err := url.Parse(ws.config.Influx_URL + ws.config.Influx_API_Path)
	if err != nil {
		return err
	}

	// Set query arguments
	query := influxURL.Query()
	query.Set("org", ws.config.Influx_Org)
	query.Set("precision", "s")
	influxURL.RawQuery = query.Encode()

	for {
		select {
		case <-ctx.Done():
			ws.logger.Info("Weather service shutting down")
			return ctx.Err()
		default:
			// Set read timeout to allow periodic context checking
			_ = ws.listener.SetReadDeadline(time.Now().Add(1 * time.Second))

			b := make([]byte, ws.config.Buffer)
			n, addr, err := ws.listener.ReadFrom(b)

			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Timeout is expected, continue to check context
					continue
				}
				udpAddr, _ := addr.(*net.UDPAddr)
				ws.logger.Error("Could not receive UDP packet",
					"remote_addr", udpAddr.String(),
					"error", err.Error())
				continue
			}

			if ws.config.Debug {
				udpAddr, _ := addr.(*net.UDPAddr)
				ws.logger.Debug("Received UDP packet",
					"remote_addr", udpAddr.String(),
					"bytes", n,
					"data", string(b[:n]))
			}

			if ws.config.Raw_UDP {
				udpAddr, _ := addr.(*net.UDPAddr)
				// Print raw bytes in hex format for tcpdump-like output
				fmt.Printf("RAW UDP: %d bytes from %s: %x\n", n, udpAddr.String(), b[:n])
			}

			// Process packet in goroutine with context
			udpAddr, _ := addr.(*net.UDPAddr)
			go processPacket(ctx, ws.config, ws.logger, influxURL, udpAddr, b, n)
		}
	}
}
