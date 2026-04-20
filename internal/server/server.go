package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"gitlab.com/sip-exporter/internal/carriers"
	"gitlab.com/sip-exporter/internal/config"
	"gitlab.com/sip-exporter/internal/exporter"
	"gitlab.com/sip-exporter/internal/service"
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

func NewServer(resolver *carriers.Resolver) Server {
	return &server{exporter: exporter.NewExporter(service.NewMetricser(), service.NewDialoger(), resolver)}
}

func (s *server) Run(cfg *config.App) error {
	if err := s.exporter.Initialize(cfg.Interface, cfg.BPFBinaryPath, cfg.SIPPort, cfg.SIPSPort); err != nil {
		return fmt.Errorf("failed initialized exporter: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if s.exporter.IsAlive() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	h := http.Server{
		Addr:              ":" + cfg.Port,
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           mux,
	}

	go func() {
		if err := h.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				zap.L().Fatal("listen", zap.Error(err))
			}
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
