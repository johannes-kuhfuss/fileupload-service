# Build container
FROM golang:1.26.1-alpine
RUN apk -U upgrade --no-cache && apk add --no-cache git && rm -rf /var/cache/apk/* && mkdir /build
WORKDIR /build
RUN git clone https://github.com/johannes-kuhfuss/fileupload-service.git
WORKDIR /build/fileupload-service
RUN go build -o /build/fileupload-service/fileupload-service /build/fileupload-service/main.go
# Run container
FROM alpine:3.23.3
RUN apk -U upgrade --no-cache && rm -rf /var/cache/apk/* && mkdir /app
WORKDIR /app
COPY --from=0 /build/fileupload-service/fileupload-service /app/fileupload-service
COPY --from=0 /build/fileupload-service/templates /app/templates
COPY --from=0 /build/fileupload-service/bootstrap /app/bootstrap
RUN addgroup -g 101 servicegroup && adduser -s /sbin/nologin -G servicegroup -D -H -u 101 serviceuser
USER serviceuser
ENV UPLOAD_PATH=/uploads
HEALTHCHECK --interval=120s --timeout=5s CMD wget -q --spider http://localhost:8080/ || exit 1
ENTRYPOINT ["/app/fileupload-service"]
EXPOSE 8080/tcp