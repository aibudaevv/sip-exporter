//go:build e2e

package e2e

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	sippFailureHelperEnv  = "SIP_EXPORTER_E2E_SIPP_FAILURE_HELPER"
	sippAttemptsFileEnv   = "SIP_EXPORTER_E2E_SIPP_ATTEMPTS_FILE"
	sippHelperExitCodeEnv = "SIP_EXPORTER_E2E_SIPP_HELPER_EXIT_CODE"
	sippScenarioChildEnv  = "SIP_EXPORTER_E2E_SIPP_SCENARIO_CHILD"
	sippStopFileEnv       = "SIP_EXPORTER_E2E_SIPP_STOP_FILE"
)

func TestMain(m *testing.M) {
	if os.Getenv(sippFailureHelperEnv) == "true" && filepath.Base(os.Args[0]) == "docker" {
		if len(os.Args) > 1 && os.Args[1] == "rm" {
			if err := os.WriteFile(os.Getenv(sippStopFileEnv), []byte("stopped\n"), 0o600); err != nil {
				os.Exit(2)
			}
			os.Exit(0)
		}
		if strings.Contains(strings.Join(os.Args, " "), "/scenarios/uas_100.xml") {
			runSippTestUAS(os.Args)
			os.Exit(0)
		}

		attemptsFile := os.Getenv(sippAttemptsFileEnv)
		file, err := os.OpenFile(attemptsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(2)
		}
		if _, err = file.WriteString("attempt\n"); err != nil {
			_ = file.Close()
			os.Exit(2)
		}
		if err = file.Close(); err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.WriteString("stdout diagnostic\n")
		_, _ = os.Stderr.WriteString("stderr diagnostic\n")
		switch os.Getenv(sippHelperExitCodeEnv) {
		case "0":
			os.Exit(0)
		case "2":
			os.Exit(2)
		}
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func runSippTestUAS(args []string) {
	for i, arg := range args {
		if arg != "-p" || i+1 >= len(args) {
			continue
		}
		listener, err := net.ListenPacket("udp", "127.0.0.1:"+args[i+1])
		if err != nil {
			os.Exit(2)
		}
		defer listener.Close()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := os.Stat(os.Getenv(sippStopFileEnv)); err == nil {
					return
				}
			case <-deadline.C:
				return
			}
		}
	}
	os.Exit(2)
}

func TestRunSippCommandInvokesCommandOnceAndPreservesDiagnostics(t *testing.T) {
	testBinary, err := os.Executable()
	require.NoError(t, err)

	binDir := t.TempDir()
	require.NoError(t, os.Symlink(testBinary, filepath.Join(binDir, "docker")))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(sippFailureHelperEnv, "true")

	tests := []struct {
		name      string
		exitCode  string
		wantError bool
	}{
		{name: "success", exitCode: "0", wantError: false},
		{name: "error", exitCode: "1", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attemptsFile := filepath.Join(t.TempDir(), "attempts")
			t.Setenv(sippAttemptsFileEnv, attemptsFile)
			t.Setenv(sippHelperExitCodeEnv, tt.exitCode)

			stdout, stderr, runErr := runSippCommand(exec.Command("docker"), io.Discard, io.Discard)

			if tt.wantError {
				require.Error(t, runErr)
			} else {
				require.NoError(t, runErr)
			}
			require.Equal(t, "stdout diagnostic", stdout)
			require.Equal(t, "stderr diagnostic", stderr)
			attempts, readErr := os.ReadFile(attemptsFile)
			require.NoError(t, readErr)
			require.Equal(t, 1, strings.Count(string(attempts), "attempt\n"))
		})
	}
}

func TestIsExpectedSippExit(t *testing.T) {
	testBinary, err := os.Executable()
	require.NoError(t, err)

	binDir := t.TempDir()
	require.NoError(t, os.Symlink(testBinary, filepath.Join(binDir, "docker")))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(sippFailureHelperEnv, "true")
	t.Setenv(sippAttemptsFileEnv, filepath.Join(t.TempDir(), "attempts"))

	t.Setenv(sippHelperExitCodeEnv, "1")
	_, _, exitOneErr := runSippCommand(exec.Command("docker"), io.Discard, io.Discard)
	require.Error(t, exitOneErr)
	t.Setenv(sippHelperExitCodeEnv, "2")
	_, _, exitTwoErr := runSippCommand(exec.Command("docker"), io.Discard, io.Discard)
	require.Error(t, exitTwoErr)

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "exit one", ctx: context.Background(), err: exitOneErr, want: true},
		{name: "canceled context", ctx: canceledCtx, err: exitOneErr, want: false},
		{name: "start failure", ctx: context.Background(), err: exec.ErrNotFound, want: false},
		{name: "wrong exit code", ctx: context.Background(), err: exitTwoErr, want: false},
		{name: "successful command", ctx: context.Background(), err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isExpectedSippExit(tt.ctx, tt.err))
		})
	}
}

func TestNextSippContainerNameIsUniquePerInvocation(t *testing.T) {
	first, err := nextSippContainerName()
	require.NoError(t, err)
	second, err := nextSippContainerName()
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestRunSippScenarioFailsAfterSingleUACAttempt(t *testing.T) {
	testBinary, err := os.Executable()
	require.NoError(t, err)

	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	stopFile := filepath.Join(t.TempDir(), "stopped")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBinary, "-test.run=^TestRunSippScenarioFailureChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		sippScenarioChildEnv+"=true",
		sippAttemptsFileEnv+"="+attemptsFile,
		sippStopFileEnv+"="+stopFile,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "normal runner contract child must not hang")
	require.Error(t, err, "normal runner must fail when its only UAC attempt fails")
	require.Contains(t, string(output), "--- FAIL: TestRunSippScenarioFailureChild")
	require.Contains(t, string(output), "stdout diagnostic")
	require.Contains(t, string(output), "stderr diagnostic")
	attempts, err := os.ReadFile(attemptsFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(attempts), "attempt\n"),
		"a failed normal SIPp scenario must not retry UAC in the same exporter")
	stopMarker, err := os.ReadFile(stopFile)
	require.NoError(t, err, "normal runner must stop UAS before reporting the UAC failure")
	require.Equal(t, "stopped\n", string(stopMarker))
}

func TestRunSippScenarioFailureChild(t *testing.T) {
	if os.Getenv(sippScenarioChildEnv) != "true" {
		t.Skip("subprocess helper")
	}

	testBinary, err := os.Executable()
	require.NoError(t, err)
	binDir := t.TempDir()
	require.NoError(t, os.Symlink(testBinary, filepath.Join(binDir, "docker")))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(sippFailureHelperEnv, "true")
	t.Setenv(sippHelperExitCodeEnv, "1")

	_, sippPort, sippClientPort := allocatePorts()
	env := &testEnv{sippPort: sippPort, sippClientPort: sippClientPort}
	runSippScenario(t.Context(), t, "uas_100.xml", "uac_100.xml", 1, env)
}

func TestRunSippUACOnlyInvokesUACOnceOnExpectedError(t *testing.T) {
	testBinary, err := os.Executable()
	require.NoError(t, err)

	binDir := t.TempDir()
	require.NoError(t, os.Symlink(testBinary, filepath.Join(binDir, "docker")))
	attemptsFile := filepath.Join(t.TempDir(), "attempts")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(sippFailureHelperEnv, "true")
	t.Setenv(sippAttemptsFileEnv, attemptsFile)
	t.Setenv(sippHelperExitCodeEnv, "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("sip_exporter_packets_total 1\n"))
	}))
	t.Cleanup(server.Close)

	env := &testEnv{
		endpoint:       server.URL,
		sippPort:       "29998",
		sippClientPort: "29999",
	}
	runSippUACOnly(context.Background(), t, "uac_100.xml", 1, env)

	attempts, err := os.ReadFile(attemptsFile)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(attempts), "attempt\n"),
		"an expected SIPp failure must not be retried in the same exporter")
}
