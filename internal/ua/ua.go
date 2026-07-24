// Package ua classifies SIP User-Agent header values into short labels via
// regex patterns loaded from YAML.
package ua

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type (
	// Pattern pairs a regex string with a human-readable label.
	Pattern struct {
		Regex string `yaml:"regex"`
		Label string `yaml:"label"`
	}

	patternEntry struct {
		re    *regexp.Regexp
		label string
	}

	// Config is the top-level YAML structure for user-agents configuration.
	Config struct {
		UserAgents []Pattern `yaml:"user_agents"`
	}

	// Classifier matches User-Agent strings against compiled regex patterns.
	Classifier struct {
		entries []patternEntry
	}
)

// LoadConfig reads and parses the user-agents YAML file at path and returns a
// configured [*Classifier].
func LoadConfig(path string) (*Classifier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read user agents config: %w", err)
	}

	var cfg Config
	if err = yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse user agents config: %w", err)
	}

	return NewClassifier(cfg.UserAgents)
}

// NewClassifier compiles a list of [Pattern] values into a [*Classifier].
func NewClassifier(patterns []Pattern) (*Classifier, error) {
	c := &Classifier{}
	for _, p := range patterns {
		if p.Label == "" {
			return nil, errors.New("user agent label is empty")
		}
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q for label %q: %w", p.Regex, p.Label, err)
		}
		c.entries = append(c.entries, patternEntry{re: re, label: p.Label})
	}
	return c, nil
}

// Classify returns the label of the first matching pattern for the given
// User-Agent string. Returns "other" if no pattern matches.
func (c *Classifier) Classify(userAgent []byte) string {
	if c == nil || len(userAgent) == 0 {
		return "other"
	}
	for _, e := range c.entries {
		if e.re.Match(userAgent) {
			return e.label
		}
	}
	return "other"
}
