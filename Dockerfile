FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY VERSION ./
COPY cmd ./cmd
COPY internal ./internal
RUN version=$(cut -d' ' -f1 VERSION) \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$version" -o /out/myapps ./cmd/myapps

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/myapps /app/myapps
EXPOSE 8080 8081
USER nonroot
ENTRYPOINT ["/app/myapps"]
