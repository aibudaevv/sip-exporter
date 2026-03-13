package main

import (
	"log"

	"gitlab.com/sip-exporter/internal/config"
	"gitlab.com/sip-exporter/internal/server"
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

	srv := server.NewServer()
	if err = srv.Run(cfg); err != nil {
		log.Fatal(err)
	}
}
