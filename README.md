# keyline-operator

A Kubernetes operator for [Keyline](https://github.com/The127/Keyline) — a self-hosted OIDC/IDP server. Declare a `KeylineInstance` and the operator deploys and manages a Keyline server; additional CRDs let you configure virtual servers, projects, applications, roles, users, and role assignments entirely through Kubernetes resources.

## Custom Resources

| Resource | Purpose |
|---|---|
| `KeylineInstance` | Deploys a Keyline server and owns all other resources |
| `KeylineVirtualServer` | Configures a virtual server (tenant) on the instance |
| `KeylineProject` | Creates a project within a virtual server |
| `KeylineApplication` | Registers an OAuth2/OIDC application in a project |
| `KeylineRole` | Defines a role in a project |
| `KeylineUser` | Creates a user on a virtual server |
| `KeylineRoleAssignment` | Assigns a user to a role |

## Installation

### Prerequisites

- Kubernetes v1.25+
- kubectl
- Helm v3

### Install with Helm

```sh
helm install keyline-operator ./charts/keyline-operator \
  --namespace keyline-operator-system \
  --create-namespace \
  --set image.tag=<version>
```

Verify the operator is running:

```sh
kubectl get pods -n keyline-operator-system
```

### Helm values

| Value | Default | Description |
|---|---|---|
| `image.repository` | `ghcr.io/the127/keyline-operator` | Container image repository |
| `image.tag` | chart `appVersion` | Image tag |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `replicaCount` | `1` | Number of operator replicas |
| `resources` | see values.yaml | Container resource requests/limits |
| `leaderElection.enabled` | `true` | Enable leader election (required for multiple replicas) |
| `metrics.enabled` | `true` | Expose Prometheus metrics |
| `metrics.port` | `8443` | Metrics server port |
| `metrics.secure` | `true` | Serve metrics over HTTPS |
| `serviceAccount.create` | `true` | Create a ServiceAccount |
| `serviceAccount.name` | `""` | Override ServiceAccount name |

### Uninstall

```sh
helm uninstall keyline-operator -n keyline-operator-system
```

> **Note:** CRDs are not deleted on uninstall. Remove them manually if needed:
> ```sh
> kubectl delete crds -l app.kubernetes.io/name=keyline-operator
> ```

## Usage

### 1. Deploy a Keyline instance

First create a Secret with your PostgreSQL credentials:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: pg-credentials
  namespace: default
stringData:
  username: keyline
  password: s3cr3t
```

Then declare a `KeylineInstance`:

```yaml
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineInstance
metadata:
  name: keyline
  namespace: default
spec:
  image: ghcr.io/the127/keyline:v1.2.3
  externalUrl: https://keyline.example.com
  frontendExternalUrl: https://app.example.com
  virtualServer: main
  database:
    mode: postgres
    postgres:
      host: postgres.default.svc
      database: keyline
      sslMode: require
      credentialsSecretRef:
        name: pg-credentials
  keyStore:
    mode: directory
    directory:
      storageSize: 1Gi
```

> **Important:** `spec.virtualServer` sets the name of the initial virtual server that the operator authenticates against. It must match the `spec.name` of your `KeylineVirtualServer` resources. The default is `keyline` — if you omit it, your `KeylineVirtualServer` names must also be `keyline`.

> **Note:** `sslMode` must match your PostgreSQL server's `pg_hba.conf`. Most managed and operator-provisioned postgres instances (e.g. Zalando) require `sslMode: require`.

The operator will generate an Ed25519 keypair, build a Keyline config, create a PVC, and deploy the server. Once ready, `status.conditions[Ready]` becomes `True` and `status.url` is populated.

```sh
kubectl get keylineinstance keyline
# NAME      IMAGE                            URL                                              READY   AGE
# keyline   ghcr.io/the127/keyline:v1.2.3   http://keyline.default.svc.cluster.local         True    2m
```

### 2. Configure a virtual server

```yaml
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineVirtualServer
metadata:
  name: main
  namespace: default
spec:
  instanceRef:
    name: keyline
  name: main
  displayName: Main
  registrationEnabled: false
  require2fa: false
```

### 3. Create a project

```yaml
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineProject
metadata:
  name: my-app
  namespace: default
spec:
  virtualServerRef:
    name: main
  slug: my-app
  name: My App
```

### 4. Register an application

```yaml
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineApplication
metadata:
  name: my-app-web
  namespace: default
spec:
  projectRef:
    name: my-app
  name: my-app-web
  displayName: My App (Web)
  type: public
  redirectUris:
    - https://app.example.com/callback
  postLogoutUris:
    - https://app.example.com
```

### 5. Create users and roles

```yaml
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineUser
metadata:
  name: alice
  namespace: default
spec:
  virtualServerRef:
    name: main
  username: alice
  displayName: Alice

---
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRole
metadata:
  name: admin
  namespace: default
spec:
  projectRef:
    name: my-app
  name: admin

---
apiVersion: keyline.keyline.dev/v1alpha1
kind: KeylineRoleAssignment
metadata:
  name: alice-admin
  namespace: default
spec:
  userRef:
    name: alice
  roleRef:
    name: admin
```

## Development

```sh
make generate    # regenerate DeepCopy methods after CRD type changes
make manifests   # regenerate CRD YAML from Go types
make install     # apply CRDs to current cluster
make run         # run controller locally against current kubeconfig
make test        # run unit test suite
make test-e2e    # run e2e tests against minikube
make lint        # run golangci-lint
```

## License

Copyright 2026. Licensed under the [GNU Affero General Public License v3.0](LICENSE).
