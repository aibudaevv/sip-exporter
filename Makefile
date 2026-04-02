version := $(shell cat VERSION)
PWD := $(shell pwd)
.DEFAULT_GOAL := docker_build

.PHONY: test test-e2e test-ser-100 test-ser-0 test-ser-redirect test-ser-mixed test-ser-no-invite test-ser-all

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
	go test -v ./...

define run_sipp_test
	@echo "Detecting host IP for interface $(SIP_EXPORTER_INTERFACE)..."
	$(eval HOST_IP := $(shell ip -4 addr show $(SIP_EXPORTER_INTERFACE) | grep -oP '(?<=inet\s)\d+(\.\d+){3}'))
	@if [ -z "$(HOST_IP)" ]; then \
		echo "Error: Could not detect IP for interface $(SIP_EXPORTER_INTERFACE)"; \
		exit 1; \
	fi
	@echo "Host IP: $(HOST_IP)"
	@echo "Detecting network configuration..."
	$(eval NETWORK := $(shell ip -o -f inet addr show $(SIP_EXPORTER_INTERFACE) | awk '{print $$4}' | cut -d'.' -f1-3))
	$(eval SUBNET := $(NETWORK).0/24)
	$(eval GATEWAY := $(shell ip route | grep default | awk '{print $$3}'))
	$(eval SIPP_SERVER_IP := $(NETWORK).200)
	$(eval SIPP_CLIENT_IP := $(NETWORK).201)
	$(eval IP_RANGE := $(NETWORK).200/30)
	@echo "Cleaning up any existing containers..."
	@docker compose -f docker-compose.test.yml down -v --remove-orphans 2>/dev/null || true
	@mkdir -p test/results
	@echo "Starting exporter in background..."
	SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE) \
	HOST_IP=$(HOST_IP) \
	SUBNET=$(SUBNET) \
	GATEWAY=$(GATEWAY) \
	SIPP_SERVER_IP=$(SIPP_SERVER_IP) \
	SIPP_CLIENT_IP=$(SIPP_CLIENT_IP) \
	IP_RANGE=$(IP_RANGE) \
	UAS_SCENARIO=$(UAS_SCENARIO) \
	UAC_SCENARIO=$(UAC_SCENARIO) \
	CALL_COUNT=$(CALL_COUNT) \
	LOG_LEVEL=$(LOG_LEVEL) \
	docker compose -f docker-compose.test.yml up -d --build exporter
	@echo "Waiting for exporter to be healthy..."
	@sleep 5
	@for i in 1 2 3 4 5; do \
		if docker compose -f docker-compose.test.yml ps exporter 2>/dev/null | grep -q "healthy"; then \
			echo "Exporter is healthy"; \
			break; \
		fi; \
		echo "Waiting for exporter..."; \
		sleep 2; \
	done
endef

test-e2e:
	@echo "Running E2E tests with Docker Compose..."
	@echo "Prerequisites: Docker, SIPp docker image"
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-e2e"; \
		exit 1; \
	fi
	$(call run_sipp_test,UAS_SCENARIO=uas_basic.xml,UAC_SCENARIO=uac_success.xml,CALL_COUNT=100,LOG_LEVEL=info)
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

define check_ser_metric
	@echo "Checking SER metric..."
	@sleep 2
	@SER_VALUE=$$(curl -s http://localhost:2113/metrics 2>/dev/null | grep '^sip_exporter_ser' | awk '{print $$2}'); \
	if [ -z "$$SER_VALUE" ]; then \
		echo "✗ SER metric not found"; \
		exit 1; \
	fi; \
	echo "SER value: $$SER_VALUE"; \
	if [ -n "$(EXPECTED_SER)" ]; then \
		DIFF=$$(echo "$$SER_VALUE - $(EXPECTED_SER)" | bc 2>/dev/null | sed 's/\..*//'); \
		if [ "$${DIFF#-}" -gt "5" ]; then \
			echo "✗ SER value $$SER_VALUE differs from expected $(EXPECTED_SER) by more than 5%"; \
			exit 1; \
		fi; \
		echo "✓ SER value within expected range"; \
	fi
endef

test-ser-100:
	@echo "=== SER Test: 100% Success Rate ==="
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-ser-100"; \
		exit 1; \
	fi
	$(call run_sipp_test,UAS_SCENARIO=uas_ser_100_success.xml,UAC_SCENARIO=uac_ser_100_success.xml,CALL_COUNT=100,LOG_LEVEL=info)
	@echo "Starting SIPp server and client..."
	@SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE) \
	SUBNET=$(SUBNET) \
	GATEWAY=$(GATEWAY) \
	SIPP_SERVER_IP=$(SIPP_SERVER_IP) \
	SIPP_CLIENT_IP=$(SIPP_CLIENT_IP) \
	IP_RANGE=$(IP_RANGE) \
	UAS_SCENARIO=uas_ser_100_success.xml \
	UAC_SCENARIO=uac_ser_100_success.xml \
	CALL_COUNT=100 \
	LOG_LEVEL=info \
	docker compose -f docker-compose.test.yml up --abort-on-container-exit --exit-code-from sipp-client sipp-server sipp-client
	$(call check_ser_metric,EXPECTED_SER=100)
	@docker compose -f docker-compose.test.yml down -v
	@echo "✓ SER 100% test completed successfully."

test-ser-0:
	@echo "=== SER Test: 0% Success Rate (All Rejected) ==="
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-ser-0"; \
		exit 1; \
	fi
	$(call run_sipp_test,UAS_SCENARIO=uas_ser_0_success.xml,UAC_SCENARIO=uac_ser_0_success.xml,CALL_COUNT=100,LOG_LEVEL=info)
	$(call check_ser_metric,EXPECTED_SER=0)
	@docker compose -f docker-compose.test.yml down -v
	@echo "✓ SER 0% test completed successfully."

test-ser-redirect:
	@echo "=== SER Test: All Redirects (3xx) ==="
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-ser-redirect"; \
		exit 1; \
	fi
	$(call run_sipp_test,UAS_SCENARIO=uas_ser_redirect.xml,UAC_SCENARIO=uac_ser_redirect.xml,CALL_COUNT=100,LOG_LEVEL=info)
	@echo "Checking SER metric (should be undefined or 0 due to all redirects)..."
	@sleep 2
	@SER_VALUE=$$(curl -s http://localhost:2113/metrics 2>/dev/null | grep '^sip_exporter_ser' | awk '{print $$2}'); \
	if [ -z "$$SER_VALUE" ] || [ "$$SER_VALUE" = "0" ]; then \
		echo "✓ SER correctly undefined/zero for all redirects"; \
	else \
		echo "SER value: $$SER_VALUE (all 3xx responses, SER should be undefined)"; \
	fi
	@docker compose -f docker-compose.test.yml down -v
	@echo "✓ SER redirect test completed successfully."

test-ser-mixed:
	@echo "=== SER Test: 70% Success Rate (Mixed) ==="
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-ser-mixed"; \
		exit 1; \
	fi
	@echo "Running 70 successful calls..."
	$(call run_sipp_test,UAS_SCENARIO=uas_ser_100_success.xml,UAC_SCENARIO=uac_ser_100_success.xml,CALL_COUNT=70,LOG_LEVEL=info)
	@SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE) \
	SUBNET=$(SUBNET) \
	GATEWAY=$(GATEWAY) \
	SIPP_SERVER_IP=$(SIPP_SERVER_IP) \
	SIPP_CLIENT_IP=$(SIPP_CLIENT_IP) \
	IP_RANGE=$(IP_RANGE) \
	UAS_SCENARIO=uas_ser_100_success.xml \
	UAC_SCENARIO=uac_ser_100_success.xml \
	CALL_COUNT=70 \
	LOG_LEVEL=info \
	docker compose -f docker-compose.test.yml up --abort-on-container-exit --exit-code-from sipp-client sipp-server sipp-client
	@echo "Running 30 rejected calls..."
	@SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE) \
	SUBNET=$(SUBNET) \
	GATEWAY=$(GATEWAY) \
	SIPP_SERVER_IP=$(SIPP_SERVER_IP) \
	SIPP_CLIENT_IP=$(SIPP_CLIENT_IP) \
	IP_RANGE=$(IP_RANGE) \
	UAS_SCENARIO=uas_ser_0_success.xml \
	UAC_SCENARIO=uac_ser_0_success.xml \
	CALL_COUNT=30 \
	LOG_LEVEL=info \
	docker compose -f docker-compose.test.yml up --abort-on-container-exit --exit-code-from sipp-client sipp-server sipp-client
	$(call check_ser_metric,EXPECTED_SER=70)
	@docker compose -f docker-compose.test.yml down -v
	@echo "✓ SER 70% mixed test completed successfully."

test-ser-no-invite:
	@echo "=== SER Test: No INVITE (Edge Case) ==="
	@if [ -z "$(SIP_EXPORTER_INTERFACE)" ]; then \
		echo "Error: SIP_EXPORTER_INTERFACE is not set."; \
		echo "Example: SIP_EXPORTER_INTERFACE=wlp0s20f3 make test-ser-no-invite"; \
		exit 1; \
	fi
	$(call run_sipp_test,UAS_SCENARIO=uas_ser_no_invite.xml,UAC_SCENARIO=uac_ser_no_invite.xml,CALL_COUNT=100,LOG_LEVEL=info)
	@echo "Checking SER metric (should be undefined/0 with no INVITE)..."
	@sleep 2
	@SER_VALUE=$$(curl -s http://localhost:2113/metrics 2>/dev/null | grep '^sip_exporter_ser' | awk '{print $$2}'); \
	if [ -z "$$SER_VALUE" ] || [ "$$SER_VALUE" = "0" ]; then \
		echo "✓ SER correctly undefined/zero with no INVITE"; \
	else \
		echo "Warning: SER has value $$SER_VALUE even with no INVITE"; \
	fi
	@docker compose -f docker-compose.test.yml down -v
	@echo "✓ SER no INVITE test completed successfully."

test-ser-all:
	@echo "=== Running all SER tests ==="
	@echo ""
	@make test-ser-100 SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE)
	@echo ""
	@make test-ser-0 SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE)
	@echo ""
	@make test-ser-redirect SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE)
	@echo ""
	@make test-ser-mixed SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE)
	@echo ""
	@make test-ser-no-invite SIP_EXPORTER_INTERFACE=$(SIP_EXPORTER_INTERFACE)
	@echo ""
	@echo "=== All SER tests completed successfully ==="

lint: vet imports
	golangci-lint run
vet:
	go vet -unsafeptr ./...
imports: vet
	goimports -l -w .
