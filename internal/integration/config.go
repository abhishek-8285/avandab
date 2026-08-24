package integration

import (
	"os"
	"strconv"

	"transport-app/internal/integration/accounting"
	"transport-app/internal/integration/ewaybill"
	"transport-app/internal/integration/fastag"
	"transport-app/internal/integration/gstn"
)

// Config holds connection settings for all integration providers.
type Config struct {
	EWayBill   ewaybill.Config
	GSTN       gstn.Config
	FASTag     fastag.Config
	Accounting accounting.Config
}

// LoadConfig reads integration settings from environment variables.
func LoadConfig() Config {
	// EWayBill supports both INTEGRATION_EWAYBILL_USE_MOCK and INTEGRATION_EWB_USE_MOCK
	ewbUseMock := os.Getenv("INTEGRATION_EWAYBILL_USE_MOCK")
	if ewbUseMock == "" {
		ewbUseMock = os.Getenv("INTEGRATION_EWB_USE_MOCK")
	}
	return Config{
		EWayBill: ewaybill.Config{
			Endpoint: os.Getenv("INTEGRATION_EWAYBILL_ENDPOINT"),
			APIKey:   getEnvDefault("INTEGRATION_EWAYBILL_API_KEY", os.Getenv("EWB_API_KEY")),
			Enabled:  parseBool(os.Getenv("INTEGRATION_EWAYBILL_ENABLED")),
			UseMock:  parseBoolDefault(ewbUseMock, true),
		},
		GSTN: gstn.Config{
			Endpoint:     os.Getenv("INTEGRATION_GSTN_ENDPOINT"),
			APIKey:       getEnvDefault("INTEGRATION_GSTN_API_KEY", os.Getenv("GSTN_API_KEY")),
			Enabled:      parseBool(os.Getenv("INTEGRATION_GSTN_ENABLED")),
			UseMock:      parseBoolDefault(os.Getenv("INTEGRATION_GSTN_USE_MOCK"), true),
			Username:     os.Getenv("INTEGRATION_GSTN_USERNAME"),
			Password:     os.Getenv("INTEGRATION_GSTN_PASSWORD"),
			ClientID:     os.Getenv("INTEGRATION_GSTN_CLIENT_ID"),
			ClientSecret: os.Getenv("INTEGRATION_GSTN_CLIENT_SECRET"),
		},
		FASTag: fastag.Config{
			Endpoint: os.Getenv("INTEGRATION_FASTAG_ENDPOINT"),
			APIKey:   getEnvDefault("INTEGRATION_FASTAG_API_KEY", os.Getenv("FASTAG_API_KEY")),
			Enabled:  parseBool(os.Getenv("INTEGRATION_FASTAG_ENABLED")),
			UseMock:  parseBoolDefault(os.Getenv("INTEGRATION_FASTAG_USE_MOCK"), true),
		},
		Accounting: accounting.Config{
			Endpoint: os.Getenv("INTEGRATION_ACCOUNTING_ENDPOINT"),
			APIKey:   os.Getenv("INTEGRATION_ACCOUNTING_API_KEY"),
			Enabled:  parseBool(os.Getenv("INTEGRATION_ACCOUNTING_ENABLED")),
			Provider: getEnvDefault("INTEGRATION_ACCOUNTING_PROVIDER", getEnvDefault("ACCOUNTING_ADAPTER", "mock")),
			UseMock:  parseBoolDefault(os.Getenv("INTEGRATION_ACCOUNTING_USE_MOCK"), true),
		},
	}
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseBool(v string) bool {
	return parseBoolDefault(v, false)
}

func parseBoolDefault(v string, def bool) bool {
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
