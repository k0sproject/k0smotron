FROM golang:1.20 as builder

ARG GOLANGCILINT_VERSION

RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v${GOLANGCILINT_VERSION}