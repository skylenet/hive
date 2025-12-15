# Build stage
# Note: Build context is hive root (set via hive_context.txt)
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev linux-headers git

WORKDIR /build

# Copy the hive module (needed for replace directive)
COPY go.mod go.sum ./
COPY hivesim ./hivesim
COPY internal ./internal

# Copy simulator source
COPY simulators/ethereum/gas-benchmark ./simulators/ethereum/gas-benchmark

WORKDIR /build/simulators/ethereum/gas-benchmark
RUN go mod download

# Build the simulator
RUN go build -o /simulator .

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates zstd

# Copy the built binary
COPY --from=builder /simulator /simulator

# Copy scenario data (path relative to hive root)
COPY simulators/ethereum/gas-benchmark/scenarios/ /scenarios/

# Copy init files (genesis, etc.)
COPY simulators/ethereum/gas-benchmark/init/ /init/

ENTRYPOINT ["/simulator"]
