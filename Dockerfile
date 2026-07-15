FROM node:22-alpine AS webbuild
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build -- --outDir /out/app --emptyOutDir

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webbuild /out/app ./cmd/junkie/static/app
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/junkie ./cmd/junkie

FROM alpine:3.22
RUN adduser -D -H junkie
WORKDIR /app
COPY --from=build /out/junkie ./junkie
COPY migrations ./migrations
USER junkie
EXPOSE 8080
CMD ["./junkie"]
