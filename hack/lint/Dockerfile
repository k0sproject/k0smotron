ARG BUILD_IMG

FROM ${BUILD_IMG} AS builder
ARG GOLANGCILINT_VERSION

RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${GOLANGCILINT_VERSION}

# Ref https://github.com/golangci/golangci-lint/pull/4034
RUN git config --global --add safe.directory '*'
