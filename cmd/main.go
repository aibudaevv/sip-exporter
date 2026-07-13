// SIP-exporter - High-performance eBPF-based SIP monitoring service
// Copyright (C) 2024 Aleksey Budaev
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"context"
	"log"
	"time"

	"go.uber.org/zap"

	"github.com/aibudaevv/sip-exporter/internal/carriers"
	"github.com/aibudaevv/sip-exporter/internal/config"
	"github.com/aibudaevv/sip-exporter/internal/geoip"
	"github.com/aibudaevv/sip-exporter/internal/server"
	"github.com/aibudaevv/sip-exporter/internal/service"
	"github.com/aibudaevv/sip-exporter/internal/telemetry"
	"github.com/aibudaevv/sip-exporter/internal/ua"
	pkgLog "github.com/aibudaevv/sip-exporter/pkg/log"
)

func main() {
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	if err = pkgLog.Verbosity(cfg.LogLevel); err != nil {
		log.Fatal(err)
	}

	var resolver *carriers.Resolver
	if cfg.CarriersConfigPath != "" {
		resolver, err = carriers.LoadConfig(cfg.CarriersConfigPath)
		if err != nil {
			log.Fatal(err)
		}
		zap.L().Info("carriers config loaded", zap.String("path", cfg.CarriersConfigPath))
	}

	var classifier *ua.Classifier
	if cfg.UserAgentsConfigPath != "" {
		classifier, err = ua.LoadConfig(cfg.UserAgentsConfigPath)
		if err != nil {
			log.Fatal(err)
		}
		zap.L().Info("user agents config loaded", zap.String("path", cfg.UserAgentsConfigPath))
	}

	geoipReader, err := geoip.New(cfg.GeoIPCountryDB)
	if err != nil {
		log.Fatal(err)
	}

	sessionsLimits := loadSessionsLimits(cfg.SessionsLimitsPath)

	srv := server.NewServer(
		resolver, classifier, geoipReader,
		cfg.LocalCountryCode, cfg.HostLabels, sessionsLimits,
		cfg.FraudRegScanThreshold, cfg.FraudRegScanWindow,
		cfg.FraudInviteBurstThreshold, cfg.FraudInviteBurstWindow,
	)

	go telemetry.Run(context.Background(), telemetry.Config{
		Enabled: cfg.Telemetry,
		URL:     cfg.TelemetryURL,
		IDFile:  cfg.TelemetryIDFile,
	}, time.Now())

	err = srv.Run(cfg)
	_ = geoipReader.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func loadSessionsLimits(path string) map[string]int {
	if path == "" {
		return nil
	}
	limits, err := service.LoadSessionsLimits(path)
	if err != nil {
		log.Fatal(err)
	}
	zap.L().Info("sessions limits loaded",
		zap.String("path", path),
		zap.Int("carriers", len(limits)))
	return limits
}
