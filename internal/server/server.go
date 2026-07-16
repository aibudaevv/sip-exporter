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

	"github.com/aibudaevv/sip-exporter/internal/carriers"
	"github.com/aibudaevv/sip-exporter/internal/config"
	"github.com/aibudaevv/sip-exporter/internal/exporter"
	"github.com/aibudaevv/sip-exporter/internal/geoip"
	"github.com/aibudaevv/sip-exporter/internal/service"
	"github.com/aibudaevv/sip-exporter/internal/ua"
)

const (
	shutDownTimeout   = 10 * time.Second
	readHeaderTimeout = 3 * time.Second
)

type (
	server struct {
		exporter    exporter.Exporter
		geoipReader *geoip.Reader
	}
	Server interface {
		Run(cfg *config.App) error
	}
	Config struct {
		Resolver                  *carriers.Resolver
		Classifier                *ua.Classifier
		GeoIPReader               *geoip.Reader
		LocalCountryCode          string
		HostLabels                bool
		SessionsLimits            map[string]int
		FraudRegScanThreshold     int
		FraudRegScanWindow        time.Duration
		FraudInviteBurstThreshold int
		FraudInviteBurstWindow    time.Duration
	}
)

func NewServer(cfg Config) Server {
	m := service.NewMetricser()
	if len(cfg.SessionsLimits) > 0 {
		m.SetSessionsLimits(cfg.SessionsLimits)
	}
	return &server{
		exporter: exporter.NewExporter(exporter.Deps{
			Metricser:                 m,
			Dialoger:                  service.NewDialoger(),
			CarrierResolver:           cfg.Resolver,
			UAClassifier:              cfg.Classifier,
			GeoIPReader:               cfg.GeoIPReader,
			LocalCountryCode:          cfg.LocalCountryCode,
			HostLabels:                cfg.HostLabels,
			FraudRegScanThreshold:     cfg.FraudRegScanThreshold,
			FraudRegScanWindow:        cfg.FraudRegScanWindow,
			FraudInviteBurstThreshold: cfg.FraudInviteBurstThreshold,
			FraudInviteBurstWindow:    cfg.FraudInviteBurstWindow,
		}),
		geoipReader: cfg.GeoIPReader,
	}
}

func (s *server) Run(cfg *config.App) error {
	if err := s.exporter.Initialize(cfg.Interface, cfg.BPFBinaryPath, cfg.SIPPort, cfg.SIPSPort, cfg.IgnoreOutgoing, cfg.RTPCapture, cfg.RTPStreamTTL); err != nil {
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
		ReadHeaderTimeout: readHeaderTimeout,
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
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range quit {
		if sig != syscall.SIGHUP {
			break
		}
		zap.L().Info("SIGHUP received, reloading GeoIP country DB")
		if err := s.geoipReader.Reload(); err != nil {
			zap.L().Warn("GeoIP country DB reload failed", zap.Error(err))
		}
	}

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
