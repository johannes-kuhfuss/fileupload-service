
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
curl -X POST http://localhost:8081/uploads \
  -H "Authorization: Bearer <keycloak-access-token>" \
  -H "Content-Type: application/json" \
  -d '{"file_name":"track.wav","file_size":104857600,"content_type":"audio/wav","checksum":"sha256:<hex-digest>"}'

curl -X PATCH http://localhost:8081/uploads/{upload_id} \
  -H "Authorization: Bearer <keycloak-access-token>" \
  -H "Upload-Offset: 0" \
  --data-binary @chunk-000.bin

curl -i http://localhost:8081/uploads/{upload_id} \
  -H "Authorization: Bearer <keycloak-access-token>"

curl -X POST http://localhost:8081/uploads/{upload_id}/complete \
  -H "Authorization: Bearer <keycloak-access-token>"
```

## Authentication and tenant boundaries

All upload API endpoints require a Keycloak-issued JWT. The service validates
the token against the configured JWKS endpoint, checks issuer and audience, and
uses the trusted `tenant_id` and `sub` claims for upload ownership.

Required scopes:

- `media:upload:create` for `POST /uploads`
- `media:upload:write` for `PATCH /uploads/{upload_id}`
- `media:upload:read` for `GET /uploads/{upload_id}`
- `media:upload:complete` for `POST /uploads/{upload_id}/complete`

Upload session metadata and quarantine paths are tenant-bound:

```text
quarantine/{tenant_id}/{upload_id}/{filename}
quarantine/_sessions/{tenant_id}/{upload_id}/metadata.json
```

Session-specific API calls only resolve sessions inside the caller's tenant.

### Local Keycloak

Build and run the local Keycloak image:

```bash
docker build -f Dockerfile.keycloak -t fileupload-keycloak .

docker run --rm \
  --name fileupload-keycloak \
  -p 8080:8080 \
  -e KEYCLOAK_ADMIN=admin \
  -e KEYCLOAK_ADMIN_PASSWORD=admin \
  fileupload-keycloak
```

The image imports `keycloak/mam-dev-realm.json` with:

- realm: `mam-dev`
- client: `fileupload-service`
- user: `developer`
- password: `developer`
- tenant claim: `tenant_id=tenant-local`
- user claim: `preferred_username=developer`
- access tokens: regular JWT access tokens with lightweight access tokens disabled

For the embedded UI, open `http://localhost:8081`, click `Login`, and sign in
with `developer` / `developer`. The UI uses the OpenID Connect authorization
code flow with PKCE and stores tokens in browser session storage.

For API-only testing, get a local access token:

```bash
curl -X POST http://localhost:8080/realms/mam-dev/protocol/openid-connect/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=password" \
  -d "client_id=fileupload-service" \
  -d "username=developer" \
  -d "password=developer"
```

Use the returned `access_token` as the `Authorization: Bearer` value in curl or
other API clients.

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
- SERVER_PORT: HTTP port for the upload service. Defaults to `8081`.
- SERVER_READ_TIMEOUT_SECONDS: Request body read timeout. Defaults to `1800`.
- SERVER_WRITE_TIMEOUT_SECONDS: Response write timeout. Defaults to `1800`.
- SERVER_IDLE_TIMEOUT_SECONDS: Idle connection timeout. Defaults to `120`.
- AUTH_ISSUER: Required JWT issuer, e.g. `http://localhost:8080/realms/mam-dev`.
- AUTH_AUDIENCE: Required JWT audience, e.g. `fileupload-service`.
- AUTH_JWKS_URL: Required Keycloak JWKS URL, e.g. `http://localhost:8080/realms/mam-dev/protocol/openid-connect/certs`.
- OTEL_EXPORTER_OTLP_ENDPOINT: Optional endpoint to send OTEL data to, e.g. "<http://192.168.1.100:4317/>". If unset, OpenTelemetry is disabled and regular logging is used.
