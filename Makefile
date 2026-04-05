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
	go test -v ./...

test-e2e:
	go test -tags=e2e -v -count=1 ./test/e2e/...

#example: make test-e2e-run TEST=TestSER_AllScenarios/100_percent
test-e2e-run:
	go test -tags=e2e -v -count=1 -run "$(TEST)" ./test/e2e/...

lint: vet imports
	golangci-lint run
vet:
	go vet -unsafeptr ./...
imports: vet
	goimports -l -w .
