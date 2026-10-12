# Cloud storage (S3-compatible)

dbk backs up to any S3-compatible object store. Put the access key ID in
`config.yaml` and the secret key in the vault:

    dbk secret set storage/<name>/secret_key --stdin   # paste the secret, Ctrl-D

Or reference an environment variable / file with `secret_key_ref`
(`env:NAME` or `file:/path`).

## AWS S3
    storages:
      - name: aws
        type: s3
        bucket: my-backups
        region: us-east-1
        access_key_id: AKIA...

## Cloudflare R2
    storages:
      - name: r2
        type: s3
        bucket: my-backups
        region: auto
        endpoint: https://<account>.r2.cloudflarestorage.com
        force_path_style: true
        access_key_id: <r2-access-key-id>

## MinIO
    storages:
      - name: minio
        type: s3
        bucket: my-backups
        region: us-east-1
        endpoint: https://minio.example.com:9000
        force_path_style: true
        access_key_id: <minio-access-key>

## Backblaze B2 (S3 API)
    storages:
      - name: b2
        type: s3
        bucket: my-backups
        region: us-west-004
        endpoint: https://s3.us-west-004.backblazeb2.com
        access_key_id: <keyID>

## Wasabi
    storages:
      - name: wasabi
        type: s3
        bucket: my-backups
        region: us-east-1
        endpoint: https://s3.wasabisys.com
        access_key_id: <wasabi-key>

## Retention

Add a `retention.keep_last` to a job to keep only the N newest backups for
that job (older ones are pruned from the storage after each successful run).
`0` or unset keeps everything.

    jobs:
      - name: main
        engine: postgres
        storage: r2
        retention:
          keep_last: 7
