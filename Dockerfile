FROM golang:1.27.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /traceforge-server ./cmd/traceforge-server
RUN CGO_ENABLED=0 go build -trimpath -o /traceforge-secure ./cmd/traceforge-secure

FROM build AS verify
COPY tests ./tests
RUN go vet ./... && go test -race ./...

FROM scratch AS secure
COPY --from=build /traceforge-secure /traceforge-secure
USER 65532:65532
ENTRYPOINT ["/traceforge-secure"]

FROM scratch AS server
COPY --from=build /traceforge-server /traceforge-server
USER 65532:65532
ENTRYPOINT ["/traceforge-server"]
CMD ["--grpc-address=0.0.0.0:4319", "--health-address=0.0.0.0:8082"]
