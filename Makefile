version := $(shell cat VERSION)
GOLANGCI_LINT_VERSION := v2.9.0
.DEFAULT_GOAL := docker_build

.PHONY: build docker_build ebpf_compile go_build clean ebpf_log lint lint-deps vet imports test test-e2e test-e2e-run test-rtp test-rtp-run test-load test-load-run test-load-rtp test-load-helper test-load-targeted test-load-release test-load-candidate test-all vulncheck trivy-fs trivy-image security

load_release_tests := ^(TestReleaseFullCallNominal|TestReleaseSoak|TestReleaseINVITEFlood|TestReleaseConcurrentDialogs|TestReleaseCarrierUA|TestReleaseMultiInterface|TestReleaseFullCallPeak|TestReleaseVQMixed)$$

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
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m ./test/e2e/load/...
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
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m ./test/e2e/load/...

test-load-run: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$(TEST)" ./test/e2e/load/...

test-load-helper: docker_build
	@test -n "$(TEST)" || (echo "TEST is required"; exit 2)
	env -u SIP_EXPORTER_LOAD_MODE -u SIP_EXPORTER_LOAD_ARTIFACT_DIR \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false \
		SIP_EXPORTER_LOAD_FINALIZE_MODE="$(SIP_EXPORTER_LOAD_FINALIZE_MODE)" \
		SIP_EXPORTER_LOAD_FINALIZE_ARTIFACT_DIR="$(ARTIFACT_DIR)" \
		SIP_EXPORTER_LOAD_FINALIZE_BASELINE="$(BASELINE)" \
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
	@test -n "$(BASELINE)" || (echo "BASELINE is required"; exit 2)
	@for run in 1 2 3; do \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false \
		SIP_EXPORTER_LOAD_MODE=release \
		SIP_EXPORTER_LOAD_ARTIFACT_DIR="$(ARTIFACT_DIR)/run-$$run" \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run '$(load_release_tests)' ./test/e2e/load/... || exit $$?; \
	done
	SIP_EXPORTER_LOAD_FINALIZE_MODE=release $(MAKE) test-load-helper \
		TEST='^TestFinalizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" BASELINE="$(BASELINE)" version="$(version)"

test-load-candidate: docker_build
	@test -n "$(ARTIFACT_DIR)" || (echo "ARTIFACT_DIR is required"; exit 2)
	@test -z "$(BASELINE)" || (echo "BASELINE is not accepted for candidate mode"; exit 2)
	@for run in 1 2 3 4 5; do \
		SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false \
		SIP_EXPORTER_LOAD_MODE=candidate \
		SIP_EXPORTER_LOAD_ARTIFACT_DIR="$(ARTIFACT_DIR)/run-$$run" \
		go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run '$(load_release_tests)' ./test/e2e/load/... || exit $$?; \
	done
	SIP_EXPORTER_LOAD_FINALIZE_MODE=candidate $(MAKE) test-load-helper \
		TEST='^TestFinalizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" version="$(version)"

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
