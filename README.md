# dingoctl

The only way to control a Dingo in the wild

## Overview

`dingoctl` is the command-line interface for managing a running Dingo node. It communicates with the node over the Bark gRPC API.

## Installation

```bash
go install github.com/blinklabs-io/dingoctl@latest
```

Or build from source:

```bash
git clone https://github.com/blinklabs-io/dingoctl.git
cd dingoctl
go build
```

## Quick Start

Connect to a local Dingo node:

```bash
dingoctl --connect localhost:8080 <command>
```

With TLS:

```bash
dingoctl --connect mainnet.example.com:443 --tls <command>
```

## Configuration

`dingoctl` supports a flexible configuration system with profiles, making it easy to manage connections to multiple Dingo nodes.

### Configuration File

Configuration is stored at `~/.config/dingoctl/config.yaml` (XDG-compliant).

Example configuration:

```yaml
current_profile: default

profiles:
  default:
    connect: localhost:8080
    timeout: 30s
    output: text

  mainnet:
    connect: mainnet.example.com:443
    tls: true
    timeout: 60s
    output: json
```

### Profile Management

```bash
# List all profiles
dingoctl config list

# Switch to a different profile
dingoctl config use mainnet

# Get a configuration value
dingoctl config get connect

# Set a configuration value
dingoctl config set connect localhost:8080
dingoctl config set --profile mainnet tls true
```

### Environment Variables

Override any configuration setting with environment variables:

```bash
export DINGOCTL_PROFILE=mainnet
export DINGOCTL_CONNECT=localhost:8080
export DINGOCTL_TIMEOUT=60s
export DINGOCTL_OUTPUT=json
```

### Priority Order

Settings are applied in this order (highest to lowest priority):

1. Command-line flags
2. Environment variables (`DINGOCTL_*`)
3. Profile settings (from config file)
4. Defaults

## Usage

```bash
dingoctl [flags] <command> [args]
```

### Commands

- `version`: Print CLI and node version information
- `config`: Manage local dingoctl profiles and settings
- `database`: Manage snapshots, restore, truncate, and operation status
- `stop`: Gracefully stop the connected Dingo node
- `restart`: Gracefully restart the connected Dingo node
- `status`: Show lifecycle state, health, uptime, and sync status

### Global Flags

- `--connect <address>`: Node address (host:port)
- `--profile <name>`: Config profile to use
- `--tls`: Use TLS
- `--insecure`: Skip TLS certificate verification
- `--ca-cert <path>`: Path to CA certificate
- `--client-cert <path>`: Path to client certificate for mTLS
- `--client-key <path>`: Path to client key for mTLS
- `--timeout <duration>`: Request timeout (e.g., 30s, 1m)
- `--output <format>`: Output format (text, json, yaml, table)
- `--quiet`: Suppress non-error output
- `--verbose`: Enable verbose output

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
  specifically. In addition, Bark's `LifecycleService` is mTLS-protected,
  so every lifecycle command (`status`, `version` node lookup, `stop`, and
  `restart`) also requires `--client-cert`/`--client-key` on such nodes.
  Without a client cert, those commands fail at the TLS/auth boundary
  before RPC dispatch. `stop`/`restart` also require an authorized operator
  principal; an authenticated but unauthorized cert is rejected with
  permission denied. Ask whoever operates the node for a certificate signed
  by its configured CA and mapped to the required role.

All of the above can also be set per-profile in the config file; see
`dingoctl config --help`.

## License

Apache 2.0 - see [LICENSE](LICENSE) for details.
