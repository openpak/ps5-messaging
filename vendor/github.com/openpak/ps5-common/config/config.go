// Package config loads the PS5 services' configuration from the environment.
package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Config is the configuration every ps5 service shares.
type Config struct {
	Addr        string // PS5_ADDR, default :8080
	LogLevel    string // PS5_LOG_LEVEL: debug, info, warn or error; default info
	PublicURL   string // PS5_PUBLIC_URL, e.g. https://ca.account.sony.com (no trailing slash)
	DatabaseURL string // PS5_DATABASE_URL, optional (postgres://...)
	TokenKey    string // PS5_TOKEN_KEY: HMAC key for oauth tokens, at least 32 bytes
	// OpenPak account core (gRPC). Both empty: no core (dev). Both set: in use.
	CoreURL   string // PS5_ACCOUNT_CORE_URL, e.g. http://127.0.0.1:7000
	CoreToken string // PS5_ACCOUNT_CORE_TOKEN, the core's internal key
}

// Load reads the environment through getenv (os.Getenv in production).
func Load(getenv func(string) string) (*Config, error) {
	c := &Config{
		Addr:        getenv("PS5_ADDR"),
		LogLevel:    strings.ToLower(getenv("PS5_LOG_LEVEL")),
		PublicURL:   strings.TrimRight(getenv("PS5_PUBLIC_URL"), "/"),
		DatabaseURL: getenv("PS5_DATABASE_URL"),
		TokenKey:    getenv("PS5_TOKEN_KEY"),
		CoreURL:     getenv("PS5_ACCOUNT_CORE_URL"),
		CoreToken:   getenv("PS5_ACCOUNT_CORE_TOKEN"),
	}
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("PS5_LOG_LEVEL must be debug, info, warn or error")
	}
	if (c.CoreURL == "") != (c.CoreToken == "") {
		return nil, fmt.Errorf("PS5_ACCOUNT_CORE_URL and PS5_ACCOUNT_CORE_TOKEN must be set together")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("PS5_PUBLIC_URL must be http(s)://host")
		}
	}
	if c.TokenKey != "" && len(c.TokenKey) < 32 {
		return nil, fmt.Errorf("PS5_TOKEN_KEY must be at least 32 bytes")
	}
	return c, nil
}
