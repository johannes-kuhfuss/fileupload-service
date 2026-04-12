
# fileupload-service

Simple File Upload Service. Allows audio files to be uploaded.

## Configuration (Environment Variables)

- UPLOAD_PATH: Folder where uploaded files are stored, e.g. "/upload" (needs to mounted as a volume in the container)
- XCODE_HOST: host name or IP where to start the transcode process, e.g. "xcode-service"
- OTEL_EXPORTER_OTLP_ENDPOINT: endpoint to send OTEL data to, e.g. "<http://192.168.1.100:4317/>"
