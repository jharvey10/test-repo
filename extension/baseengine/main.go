package baseengine

import (
	"context"

	"go.opentelemetry.io/collector/component"
)

// baseEngineExtension is the extension for the baseengine.
type baseEngineExtension struct {
	config            *Config
	telemetrySettings component.TelemetrySettings
}

// newBaseEngineExtension creates a new baseengine extension.
func newBaseEngineExtension(config *Config, telemetrySettings component.TelemetrySettings) *baseEngineExtension {
	return &baseEngineExtension{
		config:            config,
		telemetrySettings: telemetrySettings,
	}
}

// Start starts the baseengine extension.
func (e *baseEngineExtension) Start(ctx context.Context, host component.Host) error {
	return nil
}

// Shutdown shuts down the baseengine extension.
func (e *baseEngineExtension) Shutdown(ctx context.Context) error {
	return nil
}
