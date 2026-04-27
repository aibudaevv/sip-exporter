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
	"log"

	"go.uber.org/zap"

	"gitlab.com/sip-exporter/internal/carriers"
	"gitlab.com/sip-exporter/internal/config"
	"gitlab.com/sip-exporter/internal/server"
	"gitlab.com/sip-exporter/internal/ua"
	pkgLog "gitlab.com/sip-exporter/pkg/log"
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

	srv := server.NewServer(resolver, classifier)
	if err = srv.Run(cfg); err != nil {
		log.Fatal(err)
	}
}
