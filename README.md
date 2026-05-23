
# fileupload-service

Simple File Upload Service. Allows audio files to be uploaded into a quarantine
area and emits an upload event for downstream services.

## Upload flow

The service supports a resumable upload API:

- `POST /uploads` creates an upload session with the client-side SHA-256 checksum.
- `PATCH /uploads/{upload_id}` appends bytes at the `Upload-Offset` header.
- `GET /uploads/{upload_id}` returns the current offset for resume.
- `POST /uploads/{upload_id}/complete` calculates the server-side SHA-256 checksum,
  compares it with the client checksum, finalizes the upload in quarantine, and
  writes a `media.asset.uploaded.quarantined` event.

Example resumable flow:

```bash
curl -X POST http://localhost:8080/uploads \
  -H "Content-Type: application/json" \
  -d '{"file_name":"track.wav","file_size":104857600,"content_type":"audio/wav","checksum":"sha256:<hex-digest>"}'

curl -X PATCH http://localhost:8080/uploads/{upload_id} \
  -H "Upload-Offset: 0" \
  --data-binary @chunk-000.bin

curl -i http://localhost:8080/uploads/{upload_id}

curl -X POST http://localhost:8080/uploads/{upload_id}/complete
```

Uploaded bytes are stored under `QUARANTINE_PATH` while the event outbox records
what later malware scanning, metadata extraction, rendition, and catalog
services should consume. In production this outbox boundary should be connected
to the platform message broker, for example Kafka, NATS, RabbitMQ, or a cloud
event bus.

## Configuration (Environment Variables)

- UPLOAD_PATH: Folder where uploaded files are stored, e.g. "/upload" (needs to mounted as a volume in the container)
- QUARANTINE_PATH: Folder for quarantined upload files. Defaults to `UPLOAD_PATH/quarantine`
- MAX_UPLOAD_BYTES: Optional max accepted file size in bytes. `0` means unlimited.
- EVENT_OUTBOX_PATH: Optional NDJSON event outbox file. Defaults to `QUARANTINE_PATH/events/upload-events.ndjson`
- EVENT_SOURCE: Event source name. Defaults to `fileupload-service`
- SERVER_READ_TIMEOUT_SECONDS: Request body read timeout. Defaults to `1800`.
- SERVER_WRITE_TIMEOUT_SECONDS: Response write timeout. Defaults to `1800`.
- SERVER_IDLE_TIMEOUT_SECONDS: Idle connection timeout. Defaults to `120`.
- OTEL_EXPORTER_OTLP_ENDPOINT: Optional endpoint to send OTEL data to, e.g. "<http://192.168.1.100:4317/>". If unset, OpenTelemetry is disabled and regular logging is used.
