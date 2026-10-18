# Go has no Apify base image: static binary on alpine (ca-certificates for HTTPS).
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /actor .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 actor
USER actor
WORKDIR /home/actor
COPY --from=build /actor /usr/local/bin/actor
CMD ["actor"]
