# syntax=docker/dockerfile:1.7

FROM golang:1.26.4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -tags netgo,osusergo \
    -ldflags="-s -w" \
    -trimpath \
    -o /out/freedius ./cmd/freedius

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/freedius /usr/local/bin/freedius
EXPOSE 8082 8083
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/freedius"]
