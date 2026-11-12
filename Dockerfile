FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pipeline-graph .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pipeline-graph /pipeline-graph
EXPOSE 8080
ENTRYPOINT ["/pipeline-graph"]
