# Build context must be this directory (estimator module root), not the repo root.
# From repo root:
#   docker build -f estimator/Dockerfile -t kepler-estimator ./estimator
# From estimator/:
#   docker build -f Dockerfile -t kepler-estimator .
#
# Run (mount host /proc so jiffies match the node):
#   docker run --rm -v /proc:/proc:ro -e ESTIMATOR_PROC=/proc \
#     -v /tmp/kest:/tmp -e ESTIMATOR_SOCKET=/tmp/estimator.sock \
#     -e ESTIMATOR_METRICS_LISTEN=:9092 -p 9092:9092 kepler-estimator

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/estimator .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/estimator /usr/local/bin/estimator
ENTRYPOINT ["/usr/local/bin/estimator"]
