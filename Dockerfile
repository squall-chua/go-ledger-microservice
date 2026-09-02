FROM golang:1.25-alpine AS build

WORKDIR /src

# Dependencies change far less often than the code, so they get their own layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary: the runtime image has no libc to link against.
RUN CGO_ENABLED=0 go build -trimpath -o /ledger-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /ledger-server /ledger-server

# One port serves gRPC and REST together.
EXPOSE 8080

ENTRYPOINT ["/ledger-server"]
