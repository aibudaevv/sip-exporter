version := $(shell cat VERSION)
PWD := $(shell pwd)
.DEFAULT_GOAL := docker_build

.PHONY: test test-e2e

build: ebpf_compile go_build
docker_build:
	docker build  --progress=plain --no-cache -t sip-exporter:${version} .
ebpf_compile:
	clang -O2 -target bpf -c internal/bpf/sip.c -o bin/sip.o -g -fno-stack-protector
go_build:
	go build -o bin/main cmd/main.go
clean:
	rm bin/sip.o && rm bin/main
ebpf_log:
	sudo cat /sys/kernel/debug/tracing/trace_pipe
test:
	go test -v 	 ./...
test-e2e:
	@echo "Running E2E tests with Docker Compose..."
	@echo "Prerequisites: Docker, SIPp docker image"
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-e2e"; \
		exit 1; \
	fi
	@echo "Detecting host IP for interface $(SIP_EXPORTER_INTERFACE)..."
	$(eval HOST_IP := $(shell ip -4 addr show $(SIP_EXPORTER_INTERFACE) | grep -oP '(?<=inet\s)\d+(\.\d+){3}'))
	@if [ -z "$(HOST_IP)" ]; then \
		echo "Error: Could not detect IP for interface $(SIP_EXPORTER_INTERFACE)"; \
		exit 1; \
	fi
	@echo "Host IP: $(HOST_IP)"
	@echo "Cleaning up any existing containers..."
	@docker compose -f docker-compose.test.yml down -v 2>/dev/null || true
	@mkdir -p test/results
	@echo "Starting E2E test environment..."
	HOST_IP=$(HOST_IP) SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE) docker compose -f docker-compose.test.yml up --abort-on-container-exit --exit-code-from sipp-client --build
	@echo "=== Test Results ==="
	@if [ -f test/results/uac_stats.csv ]; then \
		echo "SIPp Statistics:"; \
		echo "----------------"; \
		tail -1 test/results/uac_stats.csv | awk -F';' '{print "Total Calls: " $$14 "\nSuccessful: " $$16 "\nFailed: " $$18}'; \
		if [ "$$(tail -1 test/results/uac_stats.csv | awk -F';' '{print $$18}')" = "0" ]; then \
			echo "✓ All calls successful"; \
		else \
			echo "✗ Some calls failed"; \
			exit 1; \
		fi; \
	fi
	@echo "Cleaning up..."
	@docker compose -f docker-compose.test.yml down -v
	@echo "E2E tests completed."
lint: vet imports
	golangci-lint run
vet:
	go vet -unsafeptr ./...
imports: vet
	goimports -l -w .
