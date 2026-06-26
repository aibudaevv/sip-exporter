package telemetry

import (
	"context"
	"time"

	"go.uber.org/zap"
)

const defaultBeaconInterval = 24 * time.Hour

type (
	Config struct {
		Enabled  bool
		URL      string
		IDFile   string
		Interval time.Duration
	}
)

func Run(ctx context.Context, cfg Config, startTime time.Time) {
	if !cfg.Enabled {
		zap.L().Info("telemetry disabled")
		return
	}

	anonID := getOrCreateID(cfg.IDFile)
	zap.L().Info("telemetry enabled", zap.String("anon_id", anonID))

	if err := sendBeacon(ctx, cfg.URL, beaconData{
		anonID: anonID,
		uptime: time.Since(startTime),
	}); err != nil {
		zap.L().Debug("telemetry beacon failed", zap.Error(err))
	}

	interval := cfg.Interval
	if interval == 0 {
		interval = defaultBeaconInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := sendBeacon(ctx, cfg.URL, beaconData{
				anonID: anonID,
				uptime: time.Since(startTime),
			}); err != nil {
				zap.L().Debug("telemetry beacon failed", zap.Error(err))
			}
		case <-ctx.Done():
			return
		}
	}
}
