//go:build e2e

package load

import (
	"context"
	"fmt"
	"time"
)

const (
	soakWindowDuration     = time.Minute
	soakMinimumInterval    = 2 * soakWindowDuration
	soakMinimumGrowthMB    = 8.0
	soakGrowthFraction     = 0.10
	postDrainWaitLimit     = 10 * time.Second
	postDrainPollInterval  = 100 * time.Millisecond
	postDrainStableScrapes = 2
)

type (
	soakWorkingSetGrowth struct {
		FirstMinuteMedianMB float64
		LastMinuteMedianMB  float64
		GrowthMB            float64
		AllowedGrowthMB     float64
	}

	postDrainSnapshot struct {
		ChannelLength  float64
		ActiveDialogs  float64
		ActiveTrackers float64
	}
)

func summarizeSoakWorkingSet(
	samples []resourceSample,
	start, end time.Time,
) (soakWorkingSetGrowth, error) {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return soakWorkingSetGrowth{}, fmt.Errorf("invalid soak interval")
	}
	if end.Sub(start) < soakMinimumInterval {
		return soakWorkingSetGrowth{}, fmt.Errorf("soak interval must be at least %v", soakMinimumInterval)
	}
	first, err := phaseSamples(samples, func(sample resourceSample) time.Time {
		return sample.At
	}, start, start.Add(soakWindowDuration))
	if err != nil {
		return soakWorkingSetGrowth{}, err
	}
	last, err := phaseSamples(samples, func(sample resourceSample) time.Time {
		return sample.At
	}, end.Add(-soakWindowDuration), end)
	if err != nil {
		return soakWorkingSetGrowth{}, err
	}
	if len(first) == 0 {
		return soakWorkingSetGrowth{}, fmt.Errorf("first minute has no resource samples")
	}
	if len(last) == 0 {
		return soakWorkingSetGrowth{}, fmt.Errorf("last minute has no resource samples")
	}
	firstMedian, err := workingSetMedianMB(first)
	if err != nil {
		return soakWorkingSetGrowth{}, fmt.Errorf("first minute: %w", err)
	}
	lastMedian, err := workingSetMedianMB(last)
	if err != nil {
		return soakWorkingSetGrowth{}, fmt.Errorf("last minute: %w", err)
	}
	return newSoakWorkingSetGrowth(firstMedian, lastMedian)
}

func workingSetMedianMB(samples []resourceSample) (float64, error) {
	values := make([]float64, len(samples))
	for i, sample := range samples {
		values[i] = float64(sample.WorkingSetBytes) / (1024 * 1024)
	}
	return percentile(values, 50)
}

func newSoakWorkingSetGrowth(firstMedian, lastMedian float64) (soakWorkingSetGrowth, error) {
	if !finiteFloats(firstMedian, lastMedian) {
		return soakWorkingSetGrowth{}, fmt.Errorf("working-set median is non-finite")
	}
	if firstMedian < 0 || lastMedian < 0 {
		return soakWorkingSetGrowth{}, fmt.Errorf("working-set median is negative")
	}
	growth := lastMedian - firstMedian
	allowedGrowth := max(firstMedian*soakGrowthFraction, soakMinimumGrowthMB)
	result := soakWorkingSetGrowth{
		FirstMinuteMedianMB: firstMedian,
		LastMinuteMedianMB:  lastMedian,
		GrowthMB:            growth,
		AllowedGrowthMB:     allowedGrowth,
	}
	if growth > allowedGrowth {
		return result, fmt.Errorf(
			"working-set growth %.3f MiB exceeds allowed %.3f MiB", growth, allowedGrowth,
		)
	}
	return result, nil
}

func parsePostDrainSnapshot(body []byte) (postDrainSnapshot, error) {
	channelLength, err := singleMetricValue(parseMetricSamples(body, "sip_exporter_channel_length"))
	if err != nil {
		return postDrainSnapshot{}, fmt.Errorf("channel_length: %w", err)
	}
	activeDialogs, err := singleMetricValue(parseMetricSamples(body, "sip_exporter_active_dialogs"))
	if err != nil {
		return postDrainSnapshot{}, fmt.Errorf("active_dialogs: %w", err)
	}
	activeTrackers, err := requiredMetricSum(body, "sip_exporter_active_trackers")
	if err != nil {
		return postDrainSnapshot{}, fmt.Errorf("active_trackers: %w", err)
	}
	if !finiteFloats(channelLength, activeDialogs, activeTrackers) {
		return postDrainSnapshot{}, fmt.Errorf("post-drain lifecycle gauge is non-finite")
	}
	return postDrainSnapshot{
		ChannelLength:  channelLength,
		ActiveDialogs:  activeDialogs,
		ActiveTrackers: activeTrackers,
	}, nil
}

func (s postDrainSnapshot) Validate() error {
	if !finiteFloats(s.ChannelLength, s.ActiveDialogs, s.ActiveTrackers) {
		return fmt.Errorf("post-drain lifecycle gauge is non-finite")
	}
	if s.ChannelLength != 0 {
		return fmt.Errorf("post-drain channel_length: got %v, want 0", s.ChannelLength)
	}
	if s.ActiveDialogs != 0 {
		return fmt.Errorf("post-drain active_dialogs: got %v, want 0", s.ActiveDialogs)
	}
	if s.ActiveTrackers != 0 {
		return fmt.Errorf("post-drain active_trackers: got %v, want 0", s.ActiveTrackers)
	}
	return nil
}

func waitForPostDrainSnapshot(
	ctx context.Context,
	endpoint string,
) (postDrainSnapshot, []byte, error) {
	waitCtx, cancel := context.WithTimeout(ctx, postDrainWaitLimit)
	defer cancel()
	ticker := time.NewTicker(postDrainPollInterval)
	defer ticker.Stop()

	stableScrapes := 0
	var lastErr error
	for {
		body, err := fetchMetricsBodyContext(waitCtx, endpoint)
		if err != nil {
			stableScrapes = 0
			lastErr = err
		} else {
			snapshot, parseErr := parsePostDrainSnapshot(body)
			if parseErr != nil {
				return postDrainSnapshot{}, nil, parseErr
			}
			if validateErr := snapshot.Validate(); validateErr != nil {
				stableScrapes = 0
				lastErr = validateErr
			} else {
				stableScrapes++
				if stableScrapes == postDrainStableScrapes {
					return snapshot, body, nil
				}
			}
		}

		select {
		case <-waitCtx.Done():
			return postDrainSnapshot{}, nil, fmt.Errorf(
				"wait for stable post-drain snapshot: %v: %w", lastErr, waitCtx.Err(),
			)
		case <-ticker.C:
		}
	}
}
