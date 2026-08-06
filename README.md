# dingoctl
The only way to control a Dingo in the wild

## Connecting

`dingoctl` talks to a Dingo node over Bark's gRPC API. Point it at your node
with `--connect` (or `$DINGOCTL_CONNECT`), e.g. `--connect
localhost:9091`.

### TLS and mTLS

- `--tls` (or `--insecure`, which implies it): use TLS. `--ca-cert` trusts a
  custom CA instead of the system pool.
- **`--tls` is required for every command — including read-only ones like
  `database info` — against any node that has the database lifecycle
  service enabled** (`barkPort` + `databaseLifecycle.snapshotDir`
  configured on the node). That node's TLS cert/key become mandatory
  server-side, so it no longer has a plaintext listener at all; connecting
  without `--tls` fails at the transport level before any RPC — including
  a purely read-only one — is even attempted. If you get a connection
  error on a command that used to work with no flags at all, this is
  almost always why: the node you're pointed at now requires TLS across
  the board, not just for destructive commands.
- `--client-cert`/`--client-key`: present a client certificate for mutual
  TLS. This is **additionally required** against a node that has
  `barkClientCaFilePath` configured, for `snapshot create`, `snapshot
  delete`, `snapshot verify`, `restore`, `truncate`, and `cancel`
  specifically — not just the destructive `restore`/`truncate` operations,
  since the server also authenticates resource-consuming ones like
  creating or verifying a snapshot. Without a client cert, those commands
  fail with an unauthenticated error (read-only commands like `dingoctl
  database info` still work with just `--tls`, no client cert needed). Ask
  whoever operates the node for a certificate signed by its configured CA.

All of the above can also be set per-profile in the config file; see
`dingoctl config --help`.
