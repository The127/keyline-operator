# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Kubernetes operator that manages [Keyline](../Keyline) — a self-hosted OIDC/IDP server. Modelled after the Postgres operator pattern: declaring a `KeylineInstance` CR brings a Keyline server into existence. The operator:

1. Generates an Ed25519 keypair and stores the private key in a Secret.
2. Builds a Keyline `config.yaml` (ConfigMap) with the service-user public key seeded under `initialVirtualServer.serviceUsers`.
3. Provisions a PVC for the key store (when `keyStore.mode: directory`).
4. Creates a Deployment and Service for the Keyline server.
5. Waits for the pod to be ready, then verifies connectivity via token exchange.
6. Uses the operator service-user identity to reconcile all other CRDs against the Keyline Management API.

The operator does **not** manage the database — the user supplies database connection details in the CRD spec.

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

### Bootstrap flow (per KeylineInstance)

On first reconcile the controller:

1. Generates an Ed25519 keypair; stores private key + kid + username in a Secret named `<instance>-operator-credentials`.
2. Builds `config.yaml` with the public key seeded in `initialVirtualServer.serviceUsers`; stores it in a ConfigMap named `<instance>-config`.
3. Creates a PVC named `<instance>-keys` if `keyStore.mode: directory`.
4. Creates a Deployment and ClusterIP Service. The Deployment mounts the ConfigMap and (if directory mode) the PVC.
5. Once the Deployment is available, performs a token exchange to verify the operator identity works → sets `Ready: True`.

Subsequent reconcilers derive the Keyline URL from `http://<service>.<namespace>.svc.cluster.local` and obtain tokens using `keylineclient.ServiceUserTokenSource` with the stored private key.

### KeylineInstance spec shape

```yaml
spec:
  image: ghcr.io/the127/keyline:v1.2.3   # required
  externalUrl: https://keyline.example.com # required (used in Keyline config for OIDC redirects)
  frontendExternalUrl: https://app.example.com # required
  virtualServer: keyline                   # initialVirtualServer.name, default "keyline"
  database:
    postgres:
      host: postgres.default.svc
      port: 5432                           # default 5432
      database: keyline                    # default "keyline"
      sslMode: disable                     # default "enable"
      credentialsSecretRef:
        name: pg-credentials               # must have keys: username, password
  keyStore:
    mode: directory                        # or: vault
    directory:
      storageClassName: standard           # optional
      storageSize: 1Gi                     # default "1Gi"
    vault:                                 # only when mode: vault
      address: https://vault.example.com
      mount: keyline
      prefix: ""                           # optional
      tokenSecretRef:
        name: vault-token                  # must have key: token
  resources: {}                            # optional, passed to Deployment container
```

### Custom Resources

| CRD | Keyline API surface |
|-----|---------------------|
| `KeylineInstance` | Deploys and owns a Keyline server; all other CRDs reference it |
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
