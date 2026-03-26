package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL        string
	ZitiIdentityFile   string
	ServiceToken       string
	GatewayURL         string
	HTTPAddress        string
	ZitiServiceName    string
	GatewayServiceName string
}

func FromEnv() (Config, error) {
	cfg := Config{}
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must be set")
	}
	cfg.ZitiIdentityFile = os.Getenv("ZITI_IDENTITY_FILE")
	if cfg.ZitiIdentityFile == "" {
		return Config{}, fmt.Errorf("ZITI_IDENTITY_FILE must be set")
	}
	cfg.ServiceToken = os.Getenv("SERVICE_TOKEN")
	if cfg.ServiceToken == "" {
		return Config{}, fmt.Errorf("SERVICE_TOKEN must be set")
	}
	cfg.GatewayURL = os.Getenv("GATEWAY_URL")
	if cfg.GatewayURL == "" {
		return Config{}, fmt.Errorf("GATEWAY_URL must be set")
	}
	cfg.HTTPAddress = os.Getenv("HTTP_ADDRESS")
	if cfg.HTTPAddress == "" {
		cfg.HTTPAddress = ":8080"
	}
	cfg.ZitiServiceName = os.Getenv("ZITI_SERVICE_NAME")
	if cfg.ZitiServiceName == "" {
		cfg.ZitiServiceName = "app-reminders"
	}
	cfg.GatewayServiceName = os.Getenv("GATEWAY_SERVICE_NAME")
	if cfg.GatewayServiceName == "" {
		cfg.GatewayServiceName = "gateway"
	}
	return cfg, nil
}
