package server

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gitlab.com/sip-exporter/internal/config"
	"gitlab.com/sip-exporter/internal/exporter"
	"gitlab.com/sip-exporter/internal/service"
	"go.uber.org/zap"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	shutDownTimeout = 10 * time.Second
)

type (
	server struct {
		exporter exporter.Exporter
	}
	Server interface {
		Run(cfg *config.App) error
	}
)

func NewServer() Server {
	return &server{exporter: exporter.NewExporter(service.NewMetricser(), service.NewDialoger())}
}

func (s *server) Run(cfg *config.App) error {
	if err := s.exporter.Initialize(cfg.Interface, cfg.BPFBinaryPath); err != nil {
		return fmt.Errorf("failed initialized exporter: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	h := http.Server{
		Addr:              ":" + cfg.Port,
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}

	go func() {
		if err := h.ListenAndServe(); err != nil {
			zap.L().Fatal("listen", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit

	zap.L().Info("received signal from OS for shutdown")

	ctx, cancel := context.WithTimeout(context.Background(), shutDownTimeout)
	defer cancel()

	s.exporter.Close()

	if err := h.Shutdown(ctx); err != nil {
		return err
	}

	zap.L().Info("sip-exporter gracefully shutdown")
	return nil
}
