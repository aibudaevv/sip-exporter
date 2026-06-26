package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/aibudaevv/sip-exporter/internal/version"
)

const beaconTimeout = 10 * time.Second

type (
	beaconData struct {
		anonID string
		uptime time.Duration
	}
)

func sendBeacon(ctx context.Context, beaconURL string, b beaconData) error {
	params := url.Values{}
	params.Set("anon_id", b.anonID)
	params.Set("version", version.Version)
	params.Set("os", runtime.GOOS)
	params.Set("arch", runtime.GOARCH)
	params.Set("uptime", strconv.FormatInt(int64(b.uptime.Seconds()), 10))

	u, err := url.Parse(beaconURL)
	if err != nil {
		return fmt.Errorf("parse beacon URL: %w", err)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("create beacon request: %w", err)
	}

	client := &http.Client{Timeout: beaconTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send beacon: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	return nil
}
