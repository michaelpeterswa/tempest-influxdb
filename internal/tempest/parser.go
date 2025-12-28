package tempest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"

	"github.com/de-wax/go-pkg/dewpoint"
	"github.com/jacaudi/tempest-influxdb/internal/config"
	"github.com/jacaudi/tempest-influxdb/internal/influx"
)

// Error constants for better error handling
var (
	ErrInvalidReportType   = errors.New("invalid or unsupported report type")
	ErrInsufficientData    = errors.New("insufficient observation data")
	ErrDewPointCalculation = errors.New("dewpoint calculation failed")
)

// PrecipType represents different types of precipitation
type PrecipType int

const (
	PrecipNone PrecipType = iota
	PrecipRain
	PrecipHail
	PrecipRainHail
)

// String returns the string representation of precipitation type
func (p PrecipType) String() string {
	types := []string{"none", "rain", "hail", "rain+hail"}
	if int(p) < len(types) {
		return types[p]
	}
	return "unknown"
}

// PrecipitationTypeStrings provides backward compatibility
var PrecipitationTypeStrings = []string{"none", "rain", "hail", "rain+hail"}

// Report represents a weather report from Tempest station
type Report struct {
	StationSerial    string       `json:"serial_number,omitempty"`
	ReportType       string       `json:"type"`
	HubSerial        string       `json:"hub_sn,omitempty"`
	Obs              [1][]float64 `json:"obs,omitempty"`
	Ob               [3]float64   `json:"ob,omitempty"`
	FirmwareRevision int
	Uptime           int       `json:"uptime,omitempty"`
	Timestamp        int       `json:"timestamp,omitempty"`
	ResetFlags       string    `json:"reset_flags,omitempty"`
	Seq              int       `json:"seq,omitempty"`
	Fs               []float64 `json:"fs,omitempty"`
	Radio_Stats      []float64 `json:"radio_stats,omitempty"`
	Mqtt_Stats       []float64 `json:"mqtt_stats,omitempty"`
	Voltage          float64   `json:"voltage,omitempty"`
	RSSI             float64   `json:"rssi,omitempty"`
	HubRSSI          float64   `json:"hub_rssi,omitempty"`
	SensorStatus     int       `json:"sensor_status,omitempty"`
	Debug            int       `json:"debug,omitempty"`
}

// parseObservation parses Tempest observation data
func parseObservation(cfg *config.Config, report Report, m *influx.Data) error {
	type Obs struct {
		Timestamp                 int64   // seconds
		WindLull                  float64 // m/s
		WindAvg                   float64 // m/s
		WindGust                  float64 // m/s
		WindDirection             int     // Degrees
		WindSampleInterval        int     // seconds
		StationPressure           float64 // MB
		AirTemperature            float64 // C
		RelativeHumidity          float64 // %
		Illuminance               int     // Lux
		UV                        float64 // Index
		SolarRadiation            int     // W/m*2
		PrecipitationAccumulation float64 // mm
		PrecipitationType         int     //
		StrikeAvgDistance         int     // km
		StrikeCount               int     // count
		Battery                   float64 // Voltags
		Interval                  int     // Minutes
	}
	var observation Obs

	if len(report.Obs[0]) < 18 {
		return fmt.Errorf("%w: expected 18 fields, got %d", ErrInsufficientData, len(report.Obs[0]))
	}

	data := report.Obs[0]
	observation.Timestamp = int64(data[0])
	observation.WindLull = data[1]
	observation.WindAvg = data[2]
	observation.WindGust = data[3]
	observation.WindDirection = int(math.Round(data[4]))
	observation.WindSampleInterval = int(math.Round(data[5]))
	observation.StationPressure = data[6]
	observation.AirTemperature = data[7]
	observation.RelativeHumidity = data[8]
	observation.Illuminance = int(math.Round(data[9]))
	observation.UV = data[10]
	observation.SolarRadiation = int(math.Round(data[11]))
	observation.PrecipitationAccumulation = data[12]
	observation.PrecipitationType = int(math.Round(data[13]))
	observation.StrikeAvgDistance = int(math.Round(data[14]))
	observation.StrikeCount = int(math.Round(data[15]))
	observation.Battery = data[16]
	observation.Interval = int(math.Round(data[17]))
	if cfg.Debug {
		log.Printf("OBS_ST %+v %+v", report, observation)
	}

	// Calculate Dew Point from RH and Temp
	dp, err := dewpoint.Calculate(observation.AirTemperature, observation.RelativeHumidity)
	if err != nil {
		log.Printf("dewpoint.Calculate(%f, %f): %v", observation.AirTemperature, observation.RelativeHumidity, err)
	}

	m.Timestamp = observation.Timestamp
	// Set fields and sort into alphabetical order to keep InfluxDB happy
	m.Fields = map[string]string{
		"battery":            fmt.Sprintf("%.2f", observation.Battery),
		"dew_point":          fmt.Sprintf("%.2f", dp),
		"humidity":           fmt.Sprintf("%.2f", observation.RelativeHumidity),
		"illuminance":        fmt.Sprintf("%d", observation.Illuminance),
		"p":                  fmt.Sprintf("%.2f", observation.StationPressure),
		"precipitation":      fmt.Sprintf("%.2f", observation.PrecipitationAccumulation),
		"precipitation_type": fmt.Sprintf("%d", observation.PrecipitationType),
		"solar_radiation":    fmt.Sprintf("%d", observation.SolarRadiation),
		"strike_count":       fmt.Sprintf("%d", observation.StrikeCount),
		"strike_distance":    fmt.Sprintf("%d", observation.StrikeAvgDistance),
		"temp":               fmt.Sprintf("%.2f", observation.AirTemperature),
		"uv":                 fmt.Sprintf("%.2f", observation.UV),
		"wind_avg":           fmt.Sprintf("%.2f", observation.WindAvg),
		"wind_direction":     fmt.Sprintf("%d", observation.WindDirection),
		"wind_gust":          fmt.Sprintf("%.2f", observation.WindGust),
		"wind_lull":          fmt.Sprintf("%.2f", observation.WindLull),
	}
	return nil
}

// parseRapidWind parses Tempest rapid wind data
func parseRapidWind(cfg *config.Config, report Report, m *influx.Data) error {
	type RapidWind struct {
		Timestamp     int64   // seconds
		WindSpeed     float64 // m/s
		WindDirection int     // degrees

	}
	var rapidWind RapidWind

	if len(report.Ob) < 3 {
		return fmt.Errorf("%w: expected 3 fields, got %d", ErrInsufficientData, len(report.Ob))
	}

	rapidWind.Timestamp = int64(report.Ob[0])
	rapidWind.WindSpeed = report.Ob[1]
	rapidWind.WindDirection = int(math.Round(report.Ob[2]))
	if cfg.Debug {
		log.Printf("RAPID_WIND %+v %+v", report, rapidWind)
	}

	m.Timestamp = rapidWind.Timestamp
	m.Fields = map[string]string{
		"rapid_wind_speed":     fmt.Sprintf("%.2f", rapidWind.WindSpeed),
		"rapid_wind_direction": fmt.Sprintf("%d", rapidWind.WindDirection),
	}
	return nil
}

// Parse parses weather data from Tempest station
func Parse(cfg *config.Config, addr *net.UDPAddr, b []byte, n int) (m *influx.Data, err error) {
	var report Report
	decoder := json.NewDecoder(bytes.NewReader(b[:n]))
	err = decoder.Decode(&report)
	if err != nil {
		err = fmt.Errorf("ERROR Could not Unmarshal %d bytes from %v: %v: %v", n, addr, err, string(b[:n]))
		return
	}

	m = influx.New()

	m.Bucket = cfg.Influx_Bucket

	switch report.ReportType {
	case "obs_st":
		m.Name = "weather"
		if err = parseObservation(cfg, report, m); err != nil {
			return nil, fmt.Errorf("parsing observation: %w", err)
		}
		m.Tags["station"] = report.StationSerial
	case "rapid_wind":
		if !cfg.Rapid_Wind {
			return nil, nil
		}
		m.Name = "weather"
		if err = parseRapidWind(cfg, report, m); err != nil {
			return nil, fmt.Errorf("parsing rapid wind: %w", err)
		}
		m.Tags["station"] = report.StationSerial
		if cfg.Influx_Bucket_Rapid_Wind != "" {
			m.Bucket = cfg.Influx_Bucket_Rapid_Wind
		}

	case "hub_status", "evt_precip", "evt_strike":
		return nil, nil
	default:
		return nil, nil
	}

	return
}
