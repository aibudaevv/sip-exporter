package main

import (
	"gitlab.com/sip-exporter/internal/config"
	"gitlab.com/sip-exporter/internal/server"
	"log"
)

func main() {
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	srv := server.NewServer()
	if err = srv.Run(cfg); err != nil {
		log.Fatal(err)
	}
}
