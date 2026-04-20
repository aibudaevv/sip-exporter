package carriers

import (
	"errors"
	"fmt"
	"net"
	"os"

	"gopkg.in/yaml.v3"
)

type (
	Carrier struct {
		Name  string   `yaml:"name"`
		CIDRs []string `yaml:"cidrs"`
	}

	Config struct {
		Carriers []Carrier `yaml:"carriers"`
	}

	cidrEntry struct {
		cidr    *net.IPNet
		carrier string
	}

	Resolver struct {
		entries []cidrEntry
	}
)

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
			r.entries = append(r.entries, cidrEntry{cidr: cidr, carrier: c.Name})
		}
	}
	return r, nil
}

func (r *Resolver) Lookup(ip net.IP) string {
	for _, e := range r.entries {
		if e.cidr.Contains(ip) {
			return e.carrier
		}
	}
	return "other"
}
