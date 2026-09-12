FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/asset-api .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/asset-api /usr/local/bin/asset-api
ENTRYPOINT ["/usr/local/bin/asset-api"]
CMD ["serve"]
