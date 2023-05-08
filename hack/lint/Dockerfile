ARG BUILD_IMG

FROM ${BUILD_IMG} as builder
ARG GOLANGCILINT_VERSION

RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v${GOLANGCILINT_VERSION}
