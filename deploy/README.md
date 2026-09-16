# Mender single-host deployment

The deployment builder produces Linux/amd64 binaries, immutable frontend assets,
two images, a byte inventory and recorded image IDs. It does not push or deploy
to a remote server. Configuration generation and stack startup are separate,
explicit operations. See `docs/engineering/production-deployment.md`.

No credentials, generated certificates, database state, identity-provider
simulator, or approval receipts belong in this directory. Runtime material is
generated under ignored `.local/deploy/` directories. The CA private key is
offline material and is never mounted into application containers.
