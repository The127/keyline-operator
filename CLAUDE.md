# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Kubernetes operator that manages [Keyline](../Keyline) — a self-hosted OIDC/IDP server. The operator bootstraps a service-user identity into a K8s Secret on first run, then uses that identity to reconcile custom resources against the Keyline Management API.

## Commands

Once scaffolded with kubebuilder:

```sh
make generate          # regenerate DeepCopy methods after CRD changes
make manifests         # regenerate CRD YAML from Go types
make install           # apply CRDs to current cluster
make run               # run controller locally against current kubeconfig
make docker-build      # build operator image
make test              # run envtest suite
go test ./... -run TestXxx  # run a single test
```

## Architecture

### Bootstrap flow

On startup the operator checks for a `keyline-operator` Secret in its namespace. If absent:

1. Calls `POST /api/virtual-servers/{vs}/users/service-users` with the admin credentials supplied via env/Secret.
2. Generates an Ed25519 keypair locally.
3. Registers the public key via `POST /api/virtual-servers/{vs}/users/service-users/{id}/keys`.
4. Persists the private key (PEM) and service-user metadata into a K8s Secret.

All subsequent reconcilers obtain an access token by constructing a self-signed JWT (RFC 8693 token exchange) against `POST /oidc/{virtualServer}/token` using that private key.

### Custom Resources (intended)

| CRD | Keyline API surface |
|-----|---------------------|
| `KeylineInstance` | Top-level; holds API URL, bootstrap config, ref to operator Secret |
| `KeylineVirtualServer` | `PATCH /api/virtual-servers/{name}` |
| `KeylineProject` | `/api/projects` |
| `KeylineApplication` | `/api/projects/{slug}/applications` |
| `KeylineRole` | `/api/projects/{slug}/roles` |
| `KeylineUser` | `/api/users` (regular + service users) |
| `KeylineRoleAssignment` | `/api/projects/{slug}/roles/{id}/assign` |

Each controller owns one CRD type. Status conditions follow the standard `Ready / Reason / Message` pattern. Finalizers are set on create to drive deletion via the Keyline API.

### Keyline API auth

Service users authenticate via RFC 8693 token exchange:

```
POST /oidc/{virtualServer}/token
  grant_type = urn:ietf:params:oauth:grant-type:token-exchange
  subject_token = <self-signed JWT signed with Ed25519 private key>
  subject_token_type = urn:ietf:params:oauth:token-type:access_token
```

The JWT must have `iss == sub == <service-user username>`, `aud == <application name>`, `kid == <registered key ID>`, and `scopes` including `openid`. Tokens expire in 5 minutes — fetch fresh per reconcile loop.

### Keyline API base paths

- Management API: `{externalUrl}/api/virtual-servers/{virtualServerName}/...`
- OIDC endpoints: `{externalUrl}/oidc/{virtualServerName}/...`

Virtual server name is part of every path — always carry it through controller context.
