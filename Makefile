version := $(shell cat VERSION)
.DEFAULT_GOAL := docker_build

.PHONY: test

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
lint: vet imports
	golangci-lint run
vet:
	go vet -unsafeptr ./...
imports: vet
	goimports -l -w .
