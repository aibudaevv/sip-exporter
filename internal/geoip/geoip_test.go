package geoip

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReader_EmptyPath_DisabledLookup(t *testing.T) {
	r, err := New("")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	country, ok := r.Lookup(net.ParseIP("8.8.8.8"))
	require.False(t, ok)
	require.Equal(t, "unknown", country)
}

func TestReader_EmptyPath_NilIP(t *testing.T) {
	r, err := New("")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	country, ok := r.Lookup(nil)
	require.False(t, ok)
	require.Equal(t, "unknown", country)
}

func TestReader_MissingFile_DisabledLookup(t *testing.T) {
	r, err := New(filepath.Join(t.TempDir(), "nonexistent.mmdb"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	country, ok := r.Lookup(net.ParseIP("8.8.8.8"))
	require.False(t, ok)
	require.Equal(t, "unknown", country)
}

func TestReader_CorruptFile_Error(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.mmdb")
	require.NoError(t, os.WriteFile(path, []byte("not a valid mmdb"), 0o600))

	_, err := New(path)
	require.Error(t, err)
}

func TestReader_EmptyPath_Reload_NoOp(t *testing.T) {
	r, err := New("")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	require.NoError(t, r.Reload())
}

func TestReader_MissingFile_Reload_Error(t *testing.T) {
	r, err := New(filepath.Join(t.TempDir(), "nonexistent.mmdb"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	require.Error(t, r.Reload())
}

func TestReader_DoubleClose(t *testing.T) {
	r, err := New("")
	require.NoError(t, err)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close())
}

func TestReader_Lookup_RealDB(t *testing.T) {
	dbPath := os.Getenv("GEOIP_TEST_DB")
	if dbPath == "" {
		t.Skip("set GEOIP_TEST_DB to run this test")
	}

	r, err := New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	tests := []struct {
		name string
		ip   string
		want string
		ok   bool
	}{
		{"Google DNS US", "8.8.8.8", "US", true},
		{"Cloudflare DNS AU", "1.1.1.1", "AU", true},
		{"Private IP unknown", "10.0.0.1", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			country, ok := r.Lookup(net.ParseIP(tt.ip))
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, country)
		})
	}
}

func TestReader_Reload_RealDB(t *testing.T) {
	dbPath := os.Getenv("GEOIP_TEST_DB")
	if dbPath == "" {
		t.Skip("set GEOIP_TEST_DB to run this test")
	}

	r, err := New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	country, ok := r.Lookup(net.ParseIP("8.8.8.8"))
	require.True(t, ok)
	require.Equal(t, "US", country)

	require.NoError(t, r.Reload())

	country, ok = r.Lookup(net.ParseIP("8.8.8.8"))
	require.True(t, ok)
	require.Equal(t, "US", country)
}
