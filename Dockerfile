FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/junkie ./cmd/junkie

FROM alpine:3.22
RUN adduser -D -H junkie
WORKDIR /app
COPY --from=build /out/junkie ./junkie
COPY migrations ./migrations
USER junkie
EXPOSE 8080
CMD ["./junkie"]
