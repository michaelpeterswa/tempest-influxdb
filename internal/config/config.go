package config

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/viper"

	flag "github.com/spf13/pflag"
)

// Config holds all configuration settings for the tempest influx application
type Config struct {
	Config_Dir               string `mapstructure:"CONFIG_DIR"`
	Listen_Address           string `mapstructure:"LISTEN_ADDRESS"`
	Influx_URL               string `mapstructure:"INFLUX_URL"`
	Influx_API_Path          string `mapstructure:"INFLUX_API_PATH"`
	Influx_Org               string `mapstructure:"INFLUX_ORG"`
	Influx_Token             string `mapstructure:"INFLUX_TOKEN"`
	Influx_Bucket            string `mapstructure:"INFLUX_BUCKET"`
	Influx_Bucket_Rapid_Wind string `mapstructure:"INFLUX_BUCKET_RAPID_WIND"`
	Buffer                   int
	Verbose                  bool
	Debug                    bool
	Raw_UDP                  bool `mapstructure:"RAW_UDP"`
	Noop                     bool
	Rapid_Wind               bool `mapstructure:"RAPID_WIND"`
}

// Default configuration values
const (
	DefaultListenAddress = ":50222"
	DefaultInfluxURL     = "https://localhost:8086"
	DefaultInfluxAPIPath = "/api/v2/write"
	DefaultBuffer        = 10240
	DefaultTimeout       = 10 // seconds

	// HTTP client optimization constants
	HTTPMaxIdleConns    = 100
	HTTPMaxConnsPerHost = 10
	HTTPIdleConnTimeout = 90 // seconds
)

// Validate validates the configuration and returns an error if invalid
func (c *Config) Validate() error {
	var validationErrors []string

	// Validate required fields
	if c.Influx_URL == "" {
		validationErrors = append(validationErrors, "INFLUX_URL is required")
	}

	if c.Influx_Org == "" {
		validationErrors = append(validationErrors, "INFLUX_ORG is required")
	}

	if c.Influx_Token == "" {
		validationErrors = append(validationErrors, "INFLUX_TOKEN is required")
	}

	if c.Influx_Bucket == "" {
		validationErrors = append(validationErrors, "INFLUX_BUCKET is required")
	}

	// Validate URL format
	if c.Influx_URL != "" {
		if _, err := url.Parse(c.Influx_URL); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("INFLUX_URL is not a valid URL: %v", err))
		}
	}

	// Validate listen address format
	if c.Listen_Address != "" {
		if !strings.Contains(c.Listen_Address, ":") {
			validationErrors = append(validationErrors, "LISTEN_ADDRESS must include port (e.g., ':50222')")
		}
	}

	// Validate buffer size
	if c.Buffer <= 0 {
		validationErrors = append(validationErrors, "Buffer size must be greater than 0")
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("configuration validation failed: %s", strings.Join(validationErrors, "; "))
	}

	return nil
}

// Load loads configuration from file, environment variables, and command line flags
func Load(path string, name string) *Config {
	config_file := name + ".yml"

	// Set defaults
	viper.SetDefault("Listen_Address", DefaultListenAddress)
	viper.SetDefault("Influx_URL", DefaultInfluxURL)
	viper.SetDefault("Influx_API_Path", DefaultInfluxAPIPath)
	viper.SetDefault("Buffer", DefaultBuffer)

	flag.String("listen_address", "", "Address to listen for UDP Broadcasts")
	flag.String("influx_url", "", "InfluxDB base URL (without /api/v2/write)")
	flag.String("influx_api_path", "", "InfluxDB API path (default: /api/v2/write)")
	flag.String("influx_org", "", "InfluxDB organization name")
	flag.String("influx_token", "", "Authentication token for Influx")
	flag.String("influx_bucket", "", "InfluxDB bucket name")
	flag.String("influx_bucket_rapid_wind", "", "InfluxDB bucket name for rapid wind reports")
	flag.Int("buffer", 0, "Max buffer size for the socket io")
	flag.BoolP("verbose", "v", false, "Verbose logging")
	flag.BoolP("debug", "d", false, "Debug logging")
	flag.Bool("raw_udp", false, "Show raw UDP packet data in hex format")
	flag.BoolP("noop", "n", false, "Don't post to influx")
	flag.Bool("rapid_wind", false, "Send rapid wind reports")

	viper.AddConfigPath(path)

	viper.SetConfigName(config_file)
	viper.SetConfigType("yaml")

	// Removed env prefix so INFLUX_TOKEN and INFLUX_BUCKET are read directly
	viper.AutomaticEnv()

	flag.Parse()
	if err := viper.BindPFlags(flag.CommandLine); err != nil {
		log.Fatalf("Failed to bind pflags: %v", err)
	}
	if viper.GetBool("debug") {
		viper.Set("verbose", true)
	}

	err := viper.ReadInConfig()
	if err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		} else {
			log.Fatalf("%v", err)
		}
	}

	var config *Config
	err = viper.Unmarshal(&config)
	if err != nil {
		log.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Debug print to help diagnose missing env vars
	fmt.Printf("DEBUG: INFLUX_TOKEN=\"%s\" INFLUX_BUCKET=\"%s\"\n", config.Influx_Token, config.Influx_Bucket)
	// Validate configuration using Lo library patterns
	lo.Must0(config.Validate())

	return config
}
