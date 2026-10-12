# Packages a prebuilt Linux binary; it does not compile. Build the binary first:
#   CGO_ENABLED=0 GOOS=linux GOARCH=<docker arch> go build -o bin/linux/scheduler ./cmd/scheduler
# `make image` and `make test-integration` do both steps.
FROM alpine:3.22

# wget (busybox) is used by the compose healthcheck; tzdata lets time.LoadLocation work.
RUN apk add --no-cache tzdata \
 && adduser -D -H -u 10001 scheduler

COPY bin/linux/scheduler /usr/local/bin/scheduler

USER scheduler
EXPOSE 8080
ENV HTTP_ADDR=:8080
ENTRYPOINT ["/usr/local/bin/scheduler"]
