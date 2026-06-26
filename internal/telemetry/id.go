package telemetry

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	userMessage   = "\nThis is a randomly generated anonymous ID used for telemetry.\nIt is NOT based on any personally identifiable information.\nTo reset it, delete this file. To disable telemetry, set SIP_EXPORTER_TELEMETRY=false.\n"
	maxSplitParts = 2
	idDirPerms    = 0750
	idFilePerms   = 0600
)

func getOrCreateID(path string) string {
	if path == "" {
		return uuid.New().String()
	}

	id, ok := readExistingID(path)
	if ok {
		return id
	}

	id = uuid.New().String()

	if err := os.MkdirAll(filepath.Dir(path), idDirPerms); err != nil {
		return id
	}

	content := id + "\n" + userMessage
	if err := os.WriteFile(path, []byte(content), idFilePerms); err != nil {
		return id
	}

	return id
}

func readExistingID(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	id := strings.TrimSpace(strings.SplitN(string(data), "\n", maxSplitParts)[0])
	if id == "" {
		return "", false
	}

	if _, parseErr := uuid.Parse(id); parseErr != nil {
		return "", false
	}

	return id, true
}
