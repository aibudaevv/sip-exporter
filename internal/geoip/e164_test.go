package geoip

import (
	"testing"
)

func TestLookupDestination_International(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   string
	}{
		{"RU Moscow", "+74951234567", "RU"},
		{"RU mobile", "+79161234567", "RU"},
		{"KZ Almaty", "+77271234567", "KZ"},
		{"KZ mobile", "+77001234567", "KZ"},
		{"GB London", "+442071234567", "GB"},
		{"CN Beijing", "+861012345678", "CN"},
		{"DE Berlin", "+49301234567", "DE"},
		{"FR Paris", "+33123456789", "FR"},
		{"US New York", "+12125551234", "US"},
		{"US LA", "+13105551234", "US"},
		{"CA Toronto", "+14165551234", "CA"},
		{"CA Vancouver", "+16045551234", "CA"},
		{"BS Nassau", "+12421234567", "BS"},
		{"JM Kingston", "+18761234567", "JM"},
		{"PR San Juan", "+17871234567", "PR"},
		{"00 prefix", "00442071234567", "GB"},
		{"00 prefix RU", "0074951234567", "RU"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupDestination(tt.number, "")
			if got != tt.want {
				t.Errorf("LookupDestination(%q) = %q, want %q", tt.number, got, tt.want)
			}
		})
	}
}

func TestLookupDestination_SubdivisionProof(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   string
	}{
		{"NANP US 212 → US", "+12125551234", "US"},
		{"NANP CA 416 → CA (≠US)", "+14165551234", "CA"},
		{"RU 495 → RU", "+74951234567", "RU"},
		{"KZ 727 → KZ (≠RU)", "+77271234567", "KZ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupDestination(tt.number, "")
			if got != tt.want {
				t.Errorf("LookupDestination(%q) = %q, want %q", tt.number, got, tt.want)
			}
		})
	}
}

func TestLookupDestination_LocalFallback(t *testing.T) {
	tests := []struct {
		name         string
		number       string
		localCountry string
		want         string
	}{
		{"domestic with local RU", "84951234567", "RU", "RU"},
		{"domestic with local DE", "0301234567", "DE", "DE"},
		{"domestic without local", "4951234567", "", "unknown"},
		{"short domestic without local", "1234567", "", "unknown"},
		{"empty number with local", "", "RU", "unknown"},
		{"international overrides local", "+442071234567", "RU", "GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupDestination(tt.number, tt.localCountry)
			if got != tt.want {
				t.Errorf("LookupDestination(%q, %q) = %q, want %q",
					tt.number, tt.localCountry, got, tt.want)
			}
		})
	}
}

func TestLookupDestination_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		number string
		want   string
	}{
		{"empty", "", "unknown"},
		{"just plus", "+", "unknown"},
		{"unknown prefix", "+999123456", "unknown"},
		{"spaces and dashes", "+1 (416) 555-1234", "CA"},
		{"short international", "+1", "US"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupDestination(tt.number, "")
			if got != tt.want {
				t.Errorf("LookupDestination(%q) = %q, want %q", tt.number, got, tt.want)
			}
		})
	}
}
