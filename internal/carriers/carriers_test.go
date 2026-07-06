package carriers

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolver_Lookup_SourceIP(t *testing.T) {
	r, err := NewResolver([]Carrier{
		{Name: "provider-a", CIDRs: []string{"10.0.1.0/24"}},
		{Name: "provider-b", CIDRs: []string{"10.0.2.0/24"}},
	})
	require.NoError(t, err)
	carrier, country := r.Lookup(net.ParseIP("10.0.1.5"))
	require.Equal(t, "provider-a", carrier)
	require.Empty(t, country)

	carrier, country = r.Lookup(net.ParseIP("10.0.2.100"))
	require.Equal(t, "provider-b", carrier)
	require.Empty(t, country)

	carrier, country = r.Lookup(net.ParseIP("192.168.1.1"))
	require.Equal(t, "other", carrier)
	require.Empty(t, country)
}

func TestResolver_Lookup_MultipleCIDRs(t *testing.T) {
	r, err := NewResolver([]Carrier{
		{Name: "provider-a", CIDRs: []string{"10.0.1.0/24", "192.168.100.0/24"}},
	})
	require.NoError(t, err)
	carrier, _ := r.Lookup(net.ParseIP("10.0.1.1"))
	require.Equal(t, "provider-a", carrier)

	carrier, _ = r.Lookup(net.ParseIP("192.168.100.50"))
	require.Equal(t, "provider-a", carrier)

	carrier, _ = r.Lookup(net.ParseIP("10.0.2.1"))
	require.Equal(t, "other", carrier)
}

func TestResolver_Lookup_EmptyResolver(t *testing.T) {
	r, err := NewResolver(nil)
	require.NoError(t, err)
	carrier, country := r.Lookup(net.ParseIP("10.0.1.1"))
	require.Equal(t, "other", carrier)
	require.Empty(t, country)
}

func TestResolver_Lookup_CountryField(t *testing.T) {
	r, err := NewResolver([]Carrier{
		{Name: "provider-a", CIDRs: []string{"10.0.1.0/24"}, Country: "RU"},
		{Name: "provider-b", CIDRs: []string{"10.0.2.0/24"}},
	})
	require.NoError(t, err)

	carrier, country := r.Lookup(net.ParseIP("10.0.1.5"))
	require.Equal(t, "provider-a", carrier)
	require.Equal(t, "RU", country)

	carrier, country = r.Lookup(net.ParseIP("10.0.2.100"))
	require.Equal(t, "provider-b", carrier)
	require.Empty(t, country)

	carrier, country = r.Lookup(net.ParseIP("8.8.8.8"))
	require.Equal(t, "other", carrier)
	require.Empty(t, country)
}

func TestNewResolver_InvalidCIDR(t *testing.T) {
	_, err := NewResolver([]Carrier{
		{Name: "bad", CIDRs: []string{"not-a-cidr"}},
	})
	require.Error(t, err)
}

func TestNewResolver_EmptyName(t *testing.T) {
	_, err := NewResolver([]Carrier{
		{Name: "", CIDRs: []string{"10.0.0.0/24"}},
	})
	require.Error(t, err)
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "carriers-*.yaml")
	require.NoError(t, err)

	content := []byte("carriers:\n  - name: provider-a\n    cidrs:\n      - \"10.0.1.0/24\"\n")
	_, err = tmpFile.Write(content)
	require.NoError(t, err)
	tmpFile.Close()

	r, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)
	carrier, country := r.Lookup(net.ParseIP("10.0.1.1"))
	require.Equal(t, "provider-a", carrier)
	require.Empty(t, country)
}

func TestLoadConfig_WithCountry(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "carriers-*.yaml")
	require.NoError(t, err)

	content := []byte("carriers:\n  - name: provider-a\n    country: DE\n    cidrs:\n      - \"10.0.1.0/24\"\n")
	_, err = tmpFile.Write(content)
	require.NoError(t, err)
	tmpFile.Close()

	r, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)
	carrier, country := r.Lookup(net.ParseIP("10.0.1.1"))
	require.Equal(t, "provider-a", carrier)
	require.Equal(t, "DE", country)
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/carriers.yaml")
	require.Error(t, err)
}
