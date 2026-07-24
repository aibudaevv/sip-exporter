// Package carriers maps source IP addresses to carrier names and countries
// via CIDR-based configuration loaded from YAML.
package carriers

import (
	"errors"
	"fmt"
	"net"
	"os"

	"gopkg.in/yaml.v3"
)

type (
	// Carrier defines a named operator with associated country and CIDR ranges.
	Carrier struct {
		Name    string   `yaml:"name"`
		Country string   `yaml:"country"`
		CIDRs   []string `yaml:"cidrs"`
	}

	// Config is the top-level YAML structure for carriers configuration.
	Config struct {
		Carriers []Carrier `yaml:"carriers"`
	}

	cidrEntry struct {
		cidr    *net.IPNet
		carrier string
		country string
	}

	// Resolver maps IP addresses to carrier names via CIDR matching.
	Resolver struct {
		entries []cidrEntry
	}
)

// LoadConfig reads and parses the carriers YAML file at path and returns a
// configured [*Resolver].
func LoadConfig(path string) (*Resolver, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read carriers config: %w", err)
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parse carriers config: %w", err)
	}

	return NewResolver(cfg.Carriers)
}

// NewResolver builds a [*Resolver] from a list of [Carrier] definitions.
func NewResolver(carrierList []Carrier) (*Resolver, error) {
	r := &Resolver{}
	for _, c := range carrierList {
		if c.Name == "" {
			return nil, errors.New("carrier name is empty")
		}
		for _, cidrStr := range c.CIDRs {
			_, cidr, err := net.ParseCIDR(cidrStr)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q for carrier %q: %w", cidrStr, c.Name, err)
			}
			r.entries = append(r.entries, cidrEntry{cidr: cidr, carrier: c.Name, country: c.Country})
		}
	}
	return r, nil
}

// Lookup returns the carrier name and country for the given IP. If no CIDR
// matches, returns ("other", "").
func (r *Resolver) Lookup(ip net.IP) (string, string) {
	for _, e := range r.entries {
		if e.cidr.Contains(ip) {
			return e.carrier, e.country
		}
	}
	return "other", ""
}
