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
make build
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

## License

Apache 2.0 - see [LICENSE](LICENSE) for details.

