package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPPort         int
	RTPEngineAddr    string
	WebRTCMinPort    uint16
	WebRTCMaxPort    uint16
	WebRTCNAT1To1IPs []string
	WebRTCICEAddress string
	WebRTCICEPort    int
	TelemetryEndpoint string
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment variables")
	}

	cfg := &Config{
		HTTPPort:         8081,
		RTPEngineAddr:    "127.0.0.1:22222",
		WebRTCMinPort:    50000,
		WebRTCMaxPort:    51000,
		WebRTCNAT1To1IPs: []string{"192.168.1.7"},
		WebRTCICEAddress: "192.168.1.7",
		WebRTCICEPort:    8443, // TCP
	}

	if v := os.Getenv("HTTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.HTTPPort = p
		}
	}
	if v := os.Getenv("RTPENGINE_ADDR"); v != "" {
		cfg.RTPEngineAddr = v
	}
	if v := os.Getenv("WEBRTC_MIN_PORT"); v != "" {
		if p, err := strconv.ParseUint(v, 10, 16); err == nil {
			cfg.WebRTCMinPort = uint16(p)
		}
	}
	if v := os.Getenv("WEBRTC_MAX_PORT"); v != "" {
		if p, err := strconv.ParseUint(v, 10, 16); err == nil {
			cfg.WebRTCMaxPort = uint16(p)
		}
	}
	if v := os.Getenv("WEBRTC_NAT_1TO1_IPS"); v != "" {
		cfg.WebRTCNAT1To1IPs = strings.Split(v, ",")
	}
	if v := os.Getenv("WEBRTC_ICE_ADDRESS"); v != "" {
		cfg.WebRTCICEAddress = v
	}
	if v := os.Getenv("WEBRTC_ICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.WebRTCICEPort = p
		}
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		cfg.TelemetryEndpoint = v
	}

	return cfg, nil
}

