# Go has no Apify base image: static binary on alpine (ca-certificates for HTTPS).
# The shared ruian package is vendored (vendor/), because the Apify build only
# sees this directory; refresh it with `make -C ../.. ruian-vendor`.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o /actor .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 actor
USER actor
WORKDIR /home/actor
COPY --from=build /actor /usr/local/bin/actor
CMD ["actor"]
