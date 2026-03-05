.DEFAULT_GOAL := help

.PHONY: help
help:
	@cat $(firstword $(MAKEFILE_LIST))

.PHONY: build
build:
	go build -v ./...

.PHONY: install
install: build
	go install -v ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: fmt
fmt:
	gofmt -s -w -e .

.PHONY: testacc
testacc:
	TF_ACC=1 go test -v -timeout 60m ./...

