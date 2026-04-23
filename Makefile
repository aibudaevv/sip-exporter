version := $(shell cat VERSION)
.DEFAULT_GOAL := docker_build

.PHONY: build docker_build ebpf_compile go_build clean ebpf_log lint vet imports test test-e2e test-e2e-run test-load test-load-run test-load-update-baseline vulncheck trivy-fs trivy-image security

build: ebpf_compile go_build
docker_build:
	docker inspect sip-exporter:$(version) > /dev/null 2>&1 || docker build --progress=plain -t sip-exporter:${version} .
ebpf_compile:
	clang -O2 -target bpf -c internal/bpf/sip.c -o bin/sip.o -g -fno-stack-protector
go_build:
	go build -o bin/main cmd/main.go
clean:
	rm bin/sip.o && rm bin/main
ebpf_log:
	sudo cat /sys/kernel/debug/tracing/trace_pipe
test:
	go test -v ./...

test-e2e: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 2 -failfast -timeout 10m ./test/e2e/

#example: make test-e2e-run TEST=TestSER_AllScenarios/100_percent
test-e2e-run: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -parallel 2 -failfast -timeout 10m -run "$(TEST)" ./test/e2e/

test-load: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -timeout 30m ./test/e2e/load/...

test-load-run: docker_build
	SIP_EXPORTER_E2E_IMAGE=sip-exporter:$(version) \
		TESTCONTAINERS_VERBOSE=false go test -tags=e2e -v -count=1 -timeout 30m -run "$(TEST)" ./test/e2e/load/...

test-load-update-baseline:
	cp test/e2e/load/load_result.json test/e2e/load/baseline.json

lint: vet imports
	golangci-lint run
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
