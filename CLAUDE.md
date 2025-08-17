# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Kubernetes NodePort allocator that uses a MutatingAdmissionWebhook to automatically assign NodePort ports to Services based on namespace-specific or label-based port ranges. It supports automatic allocation, port recycling, and multi-replica deployment.

Key features:
- Webhook-based port allocation for NodePort Services
- Namespace and label-based port range isolation
- Automatic port recycling when Services are deleted
- Multi-replica support with Leader Election for consistency
- Efficient port lookup using BitSet algorithm
- Persistent storage using ConfigMaps
- Scans existing NodePort Services at startup to initialize port state
- Configurable handling of ports outside defined ranges

## Architecture

The system consists of several key components:

1. **MutatingAdmissionWebhook**: Intercepts Service creation/update requests to perform port allocation and validation
2. **Controller**: Watches for Service deletion events to trigger port recycling
3. **PortManager**: Core port management component that manages all port ranges
4. **BitSet**: Efficient bitmap algorithm for port allocation lookups
5. **Leader Election**: Ensures port recycling consistency in multi-replica deployments

## Core Packages

- `pkg/admission`: Webhook implementation for port allocation and validation
- `pkg/config`: Configuration loading and validation
- `pkg/controller`: Service controller for port recycling
- `pkg/leader`: Leader election implementation
- `pkg/portmanager`: Core port management logic
- `pkg/utils`: Utility functions including BitSet implementation
- `pkg/webhook`: Webhook HTTP handlers

## Build and Development Commands

```bash
# Install dependencies
go mod tidy

# Build the binary
go build -o bin/nodeport-allocator cmd/main.go

# Run tests (if any test files exist)
go test ./...

# Run with default config
./bin/nodeport-allocator

# Run with specific config
./bin/nodeport-allocator --config config/config.yaml
```

## Configuration

The application is configured via a YAML file (default: `config/config.yaml`) that defines:
- Port ranges for different namespaces and labels
- Storage configuration (ConfigMap details)
- Default port range
- Option to allow ports outside defined ranges
- Logging level

## Key Implementation Details

- Uses BitSet data structure for efficient port allocation (O(1) lookup)
- Stores port allocation state in Kubernetes ConfigMaps
- Implements retry mechanisms for Kubernetes API conflicts
- Uses leader election to ensure only one replica handles port recycling
- Provides detailed logging and warning messages during port allocation
- Scans existing NodePort Services at startup to initialize port state
- Supports both namespace-based and label-based port range mapping
- Handles NodePort Services that are outside configured ranges based on configuration

## Startup Behavior

At startup, the application scans all existing NodePort Services in the cluster and initializes the port state in the ConfigMaps. This ensures that ports used by existing services are marked as allocated and prevents conflicts with new services.

The scanning process:
1. Lists all Services in the cluster
2. Filters for NodePort Services
3. Determines the appropriate port range for each Service based on namespace or labels
4. Marks used ports as allocated in the corresponding port range
5. Logs warnings for Services with ports outside configured ranges (if not allowed)

## Label-Based Port Range Mapping

The application supports determining port ranges based on Service labels, which allows for more flexible port management based on business domains rather than just namespaces. This is configured in the `portRanges` section of the configuration file using the `labels` field.

When determining the port range for a Service:
1. First, it checks if any port range has labels that match the Service's labels
2. If a match is found, that port range is used
3. If no label match is found, it falls back to namespace-based matching

This allows Services to be grouped into port ranges based on business domains, teams, or other organizational criteria defined by labels.