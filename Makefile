version := $(shell cat VERSION)
GOLANGCI_LINT_VERSION := v2.9.0
.DEFAULT_GOAL := docker_build

.PHONY: build docker_build ebpf_compile go_build clean ebpf_log lint lint-deps vet imports test test-e2e test-e2e-run test-rtp test-rtp-run test-load test-load-contracts test-load-diagnostic test-load-run test-load-rtp test-load-helper test-load-targeted test-load-release test-load-report test-all vulncheck trivy-fs trivy-image security

load_release_tests := ^(TestReleaseMixedNominal|TestReleaseMixedSoak|TestReleaseINVITEFlood|TestReleaseConcurrentDialogs|TestReleaseMultiInterface|TestReleaseMixedPeak|TestReleaseVQMixed)$$
load_diagnostic_sip_tests := ^(TestDiagnosticSIPFullCallNominal|TestDiagnosticSIPSoak|TestDiagnosticSIPFullCallPeak|TestDiagnosticSIPCarrierUA)$$
load_diagnostic_rtp_tests := ^(TestLoadFullCallWithRTP|TestBenchmarkMemoryPerRTPStream)$$
load_diagnostic_resource_tests := ^(TestBenchmarkMemoryPerDialog|TestBenchmarkGCPauseDuration|TestLoadMultiInterface|TestLoadVQScenarios)$$
load_workload_tests := ^(TestReleaseMixedNominal|TestReleaseMixedSoak|TestReleaseINVITEFlood|TestReleaseConcurrentDialogs|TestReleaseMultiInterface|TestReleaseMixedPeak|TestReleaseVQMixed|TestDiagnosticSIPFullCallNominal|TestDiagnosticSIPSoak|TestDiagnosticSIPFullCallPeak|TestDiagnosticSIPCarrierUA|TestLoadFullCallWithRTP|TestBenchmarkMemoryPerRTPStream|TestBenchmarkMemoryPerDialog|TestBenchmarkGCPauseDuration|TestLoadMultiInterface|TestLoadVQScenarios)$$
load_make := $(MAKE)
load_make_prefix := +@
load_make_short_flags := $(filter-out --%,$(firstword $(MAKEFLAGS)))

ifneq (,$(findstring n,$(load_make_short_flags)))
load_make_prefix :=
endif

build: ebpf_compile go_build
docker_build:
	docker inspect sip-exporter:$(version) > /dev/null 2>&1 || docker build --progress=plain -t sip-exporter:${version} .
ebpf_compile:
	clang -O2 -target bpf -c internal/bpf/sip.c -o bin/sip.o -g -fno-stack-protector
go_build:
	go build -ldflags "-X github.com/aibudaevv/sip-exporter/internal/version.Version=$(version)" -o bin/main cmd/main.go
clean:
	rm bin/sip.o && rm bin/main
ebpf_log:
	sudo cat /sys/kernel/debug/tracing/trace_pipe
test:
	go test -v ./...

.PHONY: test-docs-promql
test-docs-promql:
	SIP_EXPORTER_TEST_PROMQL=true go test -v -count=1 -run '^TestDocumentedPromQL$$' ./examples

test-all: docker_build
	@echo "=== Unit tests ==="
	go test -v ./internal/... ./pkg/...
	@echo "=== Main E2E tests ==="
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 45m ./test/e2e/
	@echo "=== RTP E2E tests ==="
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 15m ./test/e2e/rtp/
	@echo "=== Load tests ==="
	env -u SIP_EXPORTER_LOAD_MODE -u SIP_EXPORTER_LOAD_ARTIFACT_DIR \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -skip "$(load_workload_tests)" ./test/e2e/load/...
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(load_release_tests)" ./test/e2e/load/...
	@echo "=== All tests passed ==="

test-e2e: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 45m ./test/e2e/

#example: make test-e2e-run TEST=TestSERAllScenarios/100_percent
test-e2e-run: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -failfast -timeout 10m -run "$(TEST)" ./test/e2e/

# RTP e2e tests run SEPARATELY from main e2e and load tests: both create AF_PACKET
# sockets on lo, and concurrent runs cause packet loss/duplication (see AGENTS.md).
test-rtp: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 15m ./test/e2e/rtp/

test-rtp-run: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -failfast -timeout 30s -run "$(TEST)" ./test/e2e/rtp/

test-load: docker_build
	env -u SIP_EXPORTER_LOAD_MODE -u SIP_EXPORTER_LOAD_ARTIFACT_DIR \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -skip "$(load_workload_tests)" ./test/e2e/load/...
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(load_release_tests)" ./test/e2e/load/...

test-load-contracts: docker_build
	env -u SIP_EXPORTER_LOAD_MODE -u SIP_EXPORTER_LOAD_ARTIFACT_DIR \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -skip "$(load_workload_tests)" ./test/e2e/load/...

test-load-diagnostic: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(load_diagnostic_sip_tests)" ./test/e2e/load/...
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(load_diagnostic_rtp_tests)" ./test/e2e/load/...
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(load_diagnostic_resource_tests)" ./test/e2e/load/...

test-load-run: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(TEST)" ./test/e2e/load/...

test-load-helper: docker_build
	@test -n "$(TEST)" || (echo "TEST is required"; exit 2)
	env -u SIP_EXPORTER_LOAD_MODE -u SIP_EXPORTER_LOAD_ARTIFACT_DIR \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false \
		SIP_EXPORTER_LOAD_SUMMARY_ARTIFACT_DIR="$(ARTIFACT_DIR)" \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(TEST)" ./test/e2e/load/...

test-load-targeted: docker_build
	@test -n "$(TEST)" || (echo "TEST is required"; exit 2)
	@test -n "$(ARTIFACT_DIR)" || (echo "ARTIFACT_DIR is required"; exit 2)
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false \
		SIP_EXPORTER_LOAD_MODE=targeted \
		SIP_EXPORTER_LOAD_ARTIFACT_DIR="$(ARTIFACT_DIR)" \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(TEST)" ./test/e2e/load/...

test-load-release: docker_build
	@test -n "$(ARTIFACT_DIR)" || (echo "ARTIFACT_DIR is required"; exit 2)
	@mkdir -p "$(ARTIFACT_DIR)"
	$(load_make_prefix)status=0; \
	mkdir -p "$(ARTIFACT_DIR)/run-1"; \
	ARTIFACT_DIR="$(ARTIFACT_DIR)" bash -o pipefail -c 'SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false SIP_EXPORTER_LOAD_MODE=release SIP_EXPORTER_LOAD_ARTIFACT_DIR="$$ARTIFACT_DIR/run-1" go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(load_release_tests)" ./test/e2e/load/... 2>&1 | tee "$$ARTIFACT_DIR/run-1/go-test.log"'; status=$$?; \
	printf '%s\n' $$status > "$(ARTIFACT_DIR)/run-1/go-test.exit-code"; \
	$(load_make) test-load-report ARTIFACT_DIR="$(ARTIFACT_DIR)" version="$(version)"; report_status=$$?; \
	if test $$status -ne 0; then exit $$status; fi; exit $$report_status

test-load-report: docker_build
	@test -n "$(ARTIFACT_DIR)" || (echo "ARTIFACT_DIR is required"; exit 2)
	$(load_make_prefix)$(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" version="$(version)"

test-load-rtp: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 10m -run 'TestLoadFullCallWithRTP|TestBenchmarkMemoryPerRTPStream' ./test/e2e/load/...

lint: vet imports
	golangci-lint run
lint-deps:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
vet:
	go vet -unsafeptr ./...
imports: vet
	goimports -l -w .

vulncheck:
	govulncheck ./...

trivy-fs:
	trivy fs .

trivy-image: docker_build
	trivy image sip-exporter:$(version)

security: vulncheck trivy-fs
