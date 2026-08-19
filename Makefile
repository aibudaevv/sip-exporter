version := $(shell cat VERSION)
GOLANGCI_LINT_VERSION := v2.9.0
.DEFAULT_GOAL := docker_build

.PHONY: build docker_build ebpf_compile go_build clean ebpf_log lint lint-deps vet imports test test-e2e test-e2e-run test-rtp test-rtp-run test-load test-load-run test-load-rtp test-load-helper test-load-targeted test-load-release test-load-candidate test-all vulncheck trivy-fs trivy-image security

load_release_tests := ^(TestReleaseFullCallNominal|TestReleaseSoak|TestReleaseINVITEFlood|TestReleaseConcurrentDialogs|TestReleaseCarrierUA|TestReleaseMultiInterface|TestReleaseFullCallPeak|TestReleaseVQMixed)$$
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
		SIP_EXPORTER_LOAD_PREFLIGHT_MODE="$(SIP_EXPORTER_LOAD_PREFLIGHT_MODE)" \
		SIP_EXPORTER_LOAD_PREFLIGHT_BASELINE="$(BASELINE)" \
		SIP_EXPORTER_LOAD_SUMMARY_MODE="$(SIP_EXPORTER_LOAD_SUMMARY_MODE)" \
		SIP_EXPORTER_LOAD_SUMMARY_ARTIFACT_DIR="$(ARTIFACT_DIR)" \
		SIP_EXPORTER_LOAD_SUMMARY_BASELINE="$(BASELINE)" \
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
	@mkdir -p "$(ARTIFACT_DIR)"
	$(load_make_prefix)ARTIFACT_DIR="$(ARTIFACT_DIR)" BASELINE="$(BASELINE)" bash -o pipefail -c 'SIP_EXPORTER_LOAD_PREFLIGHT_MODE=release $(load_make) test-load-helper TEST="^TestPreflightLoadMode$$" ARTIFACT_DIR="$$ARTIFACT_DIR" BASELINE="$$BASELINE" version="$(version)" 2>&1 | tee "$$ARTIFACT_DIR/preflight.log"'; status=$$?; \
	printf '%s\n' $$status > "$(ARTIFACT_DIR)/preflight.exit-code"; \
	if test $$status -ne 0; then SIP_EXPORTER_LOAD_SUMMARY_MODE=release $(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" BASELINE="$(BASELINE)" version="$(version)"; exit $$status; fi
	$(load_make_prefix)status=0; for run in 1 2 3; do \
		mkdir -p "$(ARTIFACT_DIR)/run-$$run"; \
		ARTIFACT_DIR="$(ARTIFACT_DIR)" RUN="$$run" TEST_PATTERN="$(load_release_tests)" bash -o pipefail -c 'SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false SIP_EXPORTER_LOAD_MODE=release SIP_EXPORTER_LOAD_ARTIFACT_DIR="$$ARTIFACT_DIR/run-$$RUN" go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$$TEST_PATTERN" ./test/e2e/load/... 2>&1 | tee "$$ARTIFACT_DIR/run-$$RUN/go-test.log"'; code=$$?; \
		printf '%s\n' $$code > "$(ARTIFACT_DIR)/run-$$run/go-test.exit-code"; \
		if test $$code -ne 0; then status=$$code; break; fi; \
	done; \
	if test $$status -ne 0; then SIP_EXPORTER_LOAD_SUMMARY_MODE=release $(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" BASELINE="$(BASELINE)" version="$(version)"; exit $$status; fi
	$(load_make_prefix)ARTIFACT_DIR="$(ARTIFACT_DIR)" BASELINE="$(BASELINE)" bash -o pipefail -c 'SIP_EXPORTER_LOAD_FINALIZE_MODE=release $(load_make) test-load-helper TEST="^TestFinalizeLoadMode$$" ARTIFACT_DIR="$$ARTIFACT_DIR" BASELINE="$$BASELINE" version="$(version)" 2>&1 | tee "$$ARTIFACT_DIR/finalize.log"'; status=$$?; \
	printf '%s\n' $$status > "$(ARTIFACT_DIR)/finalize.exit-code"; \
	SIP_EXPORTER_LOAD_SUMMARY_MODE=release $(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" BASELINE="$(BASELINE)" version="$(version)"; summary_status=$$?; \
	if test $$status -eq 0 && test $$summary_status -ne 0; then exit $$summary_status; fi; exit $$status

test-load-candidate: docker_build
	@test -n "$(ARTIFACT_DIR)" || (echo "ARTIFACT_DIR is required"; exit 2)
	@test -z "$(BASELINE)" || (echo "BASELINE is not accepted for candidate mode"; exit 2)
	@mkdir -p "$(ARTIFACT_DIR)"
	$(load_make_prefix)ARTIFACT_DIR="$(ARTIFACT_DIR)" bash -o pipefail -c 'SIP_EXPORTER_LOAD_PREFLIGHT_MODE=candidate $(load_make) test-load-helper TEST="^TestPreflightLoadMode$$" ARTIFACT_DIR="$$ARTIFACT_DIR" version="$(version)" 2>&1 | tee "$$ARTIFACT_DIR/preflight.log"'; status=$$?; \
	printf '%s\n' $$status > "$(ARTIFACT_DIR)/preflight.exit-code"; \
	if test $$status -ne 0; then SIP_EXPORTER_LOAD_SUMMARY_MODE=candidate $(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" version="$(version)"; exit $$status; fi
	$(load_make_prefix)status=0; for run in 1 2 3 4 5; do \
		mkdir -p "$(ARTIFACT_DIR)/run-$$run"; \
		ARTIFACT_DIR="$(ARTIFACT_DIR)" RUN="$$run" TEST_PATTERN="$(load_release_tests)" bash -o pipefail -c 'SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) TESTCONTAINERS_VERBOSE=false SIP_EXPORTER_LOAD_MODE=candidate SIP_EXPORTER_LOAD_ARTIFACT_DIR="$$ARTIFACT_DIR/run-$$RUN" go test -tags=e2e -v -count=1 -parallel 1 -timeout 30m -run "$$TEST_PATTERN" ./test/e2e/load/... 2>&1 | tee "$$ARTIFACT_DIR/run-$$RUN/go-test.log"'; code=$$?; \
		printf '%s\n' $$code > "$(ARTIFACT_DIR)/run-$$run/go-test.exit-code"; \
		if test $$code -ne 0; then status=$$code; break; fi; \
	done; \
	if test $$status -ne 0; then SIP_EXPORTER_LOAD_SUMMARY_MODE=candidate $(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" version="$(version)"; exit $$status; fi
	$(load_make_prefix)ARTIFACT_DIR="$(ARTIFACT_DIR)" bash -o pipefail -c 'SIP_EXPORTER_LOAD_FINALIZE_MODE=candidate $(load_make) test-load-helper TEST="^TestFinalizeLoadMode$$" ARTIFACT_DIR="$$ARTIFACT_DIR" version="$(version)" 2>&1 | tee "$$ARTIFACT_DIR/finalize.log"'; status=$$?; \
	printf '%s\n' $$status > "$(ARTIFACT_DIR)/finalize.exit-code"; \
	SIP_EXPORTER_LOAD_SUMMARY_MODE=candidate $(load_make) test-load-helper TEST='^TestSummarizeLoadMode$$' ARTIFACT_DIR="$(ARTIFACT_DIR)" version="$(version)"; summary_status=$$?; \
	if test $$status -eq 0 && test $$summary_status -ne 0; then exit $$summary_status; fi; exit $$status

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
