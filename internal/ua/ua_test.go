package ua

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifierClassifyYealink(t *testing.T) {
	c, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Yealink`, Label: "yealink"},
		{Regex: `(?i)^Grandstream`, Label: "grandstream"},
	})
	require.NoError(t, err)
	require.Equal(t, "yealink", c.Classify([]byte("Yealink SIP-T46S 66.15.0.10")))
}

func TestClassifierClassifyGrandstream(t *testing.T) {
	c, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Yealink`, Label: "yealink"},
		{Regex: `(?i)^Grandstream`, Label: "grandstream"},
	})
	require.NoError(t, err)
	require.Equal(t, "grandstream", c.Classify([]byte("Grandstream GXP2160 1.0.9.50")))
}

func TestClassifierClassifyNoMatch(t *testing.T) {
	c, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Yealink`, Label: "yealink"},
	})
	require.NoError(t, err)
	require.Equal(t, "other", c.Classify([]byte("UnknownClient/1.0")))
}

func TestClassifierClassifyEmptyInput(t *testing.T) {
	c, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Yealink`, Label: "yealink"},
	})
	require.NoError(t, err)
	require.Equal(t, "other", c.Classify(nil))
	require.Equal(t, "other", c.Classify([]byte{}))
}

func TestClassifierClassifyNilClassifier(t *testing.T) {
	var c *Classifier
	require.Equal(t, "other", c.Classify([]byte("Yealink SIP-T46S")))
}

func TestClassifierEmptyPatterns(t *testing.T) {
	c, err := NewClassifier(nil)
	require.NoError(t, err)
	require.Equal(t, "other", c.Classify([]byte("Yealink SIP-T46S")))
}

func TestClassifierInvalidRegex(t *testing.T) {
	_, err := NewClassifier([]Pattern{
		{Regex: `[invalid`, Label: "bad"},
	})
	require.Error(t, err)
}

func TestClassifierEmptyLabel(t *testing.T) {
	_, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Yealink`, Label: ""},
	})
	require.Error(t, err)
}

func TestClassifierFirstMatchWins(t *testing.T) {
	c, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Yealink`, Label: "yealink"},
		{Regex: `(?i)^Y`, Label: "y-vendor"},
	})
	require.NoError(t, err)
	require.Equal(t, "yealink", c.Classify([]byte("Yealink SIP-T46S")))
}

func TestClassifierCaseInsensitive(t *testing.T) {
	c, err := NewClassifier([]Pattern{
		{Regex: `(?i)^Kamailio`, Label: "kamailio"},
	})
	require.NoError(t, err)
	require.Equal(t, "kamailio", c.Classify([]byte("KAMAILIO (5.7.4)")))
	require.Equal(t, "kamailio", c.Classify([]byte("kamailio (5.7.4)")))
}

func TestLoadConfigValidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "user-agents-*.yaml")
	require.NoError(t, err)

	content := []byte("user_agents:\n  - regex: '(?i)^Yealink'\n    label: yealink\n")
	_, err = tmpFile.Write(content)
	require.NoError(t, err)
	tmpFile.Close()

	c, err := LoadConfig(tmpFile.Name())
	require.NoError(t, err)
	require.Equal(t, "yealink", c.Classify([]byte("Yealink SIP-T46S")))
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/user_agents.yaml")
	require.Error(t, err)
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "user-agents-*.yaml")
	require.NoError(t, err)

	_, err = tmpFile.WriteString("not: [valid: yaml")
	require.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	require.Error(t, err)
}

func TestLoadConfigInvalidRegexInFile(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "user-agents-*.yaml")
	require.NoError(t, err)

	content := []byte("user_agents:\n  - regex: '[invalid'\n    label: bad\n")
	_, err = tmpFile.Write(content)
	require.NoError(t, err)
	tmpFile.Close()

	_, err = LoadConfig(tmpFile.Name())
	require.Error(t, err)
}
