package geoip

import (
	"fmt"
	"net"
	"sync"

	"github.com/oschwald/maxminddb-golang"
	"go.uber.org/zap"
)

const unknownCountry = "unknown"

type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

type Reader struct {
	db   *maxminddb.Reader
	path string
	mu   sync.RWMutex
}

func New(path string) (*Reader, error) {
	if path == "" {
		zap.L().
			Warn(`GeoIP country DB not configured; source_country will be "unknown" for traffic without carrier.country`)
		return &Reader{}, nil
	}

	db, err := maxminddb.Open(path)
	if err != nil {
		zap.L().
			Warn(`GeoIP country DB failed to open; source_country will be "unknown" for traffic without carrier.country`,
				zap.String("path", path), zap.Error(err))
		return &Reader{path: path}, nil
	}

	zap.L().Info("GeoIP country DB loaded", zap.String("path", path))
	return &Reader{db: db, path: path}, nil
}

func (r *Reader) Lookup(ip net.IP) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.db == nil {
		return unknownCountry, false
	}

	var rec countryRecord
	if err := r.db.Lookup(ip, &rec); err != nil {
		return unknownCountry, false
	}

	if rec.Country.ISOCode == "" {
		return unknownCountry, false
	}

	return rec.Country.ISOCode, true
}

func (r *Reader) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.path == "" {
		return nil
	}

	db, err := maxminddb.Open(r.path)
	if err != nil {
		return fmt.Errorf("reload geoip db %q: %w", r.path, err)
	}

	if r.db != nil {
		_ = r.db.Close()
	}
	r.db = db

	zap.L().Info("GeoIP country DB reloaded", zap.String("path", r.path))
	return nil
}

func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.db == nil {
		return nil
	}

	err := r.db.Close()
	r.db = nil
	return err
}
