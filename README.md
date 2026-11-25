# SimpleApp

[![Docker](https://github.com/pecio/simpleapp/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/pecio/simpleapp/actions/workflows/docker-publish.yml)

**SimpleApp** is a small Kubernetes controller that turns a tiny custom resource into a
`Deployment` and a `Service`. Declare an image, a few ports, and optional volumes, and the
controller creates and keeps the workload in sync.

It is deliberately minimal — a readable example of a Kubernetes controller rather than a
production-grade operator.

```text
SimpleApp CR ──▶ controller ──▶ Deployment (1 container per pod)
                          └──▶ Service    (ClusterIP / NodePort / LoadBalancer)
```

- **API group:** `apps.raulpedroche.es`
- **Version:** `v1alpha1`
- **Kind:** `SimpleApp` (namespaced)

## Features

- Single-container `Deployment` generated from the `SimpleApp` spec (image, replicas, env, ports, volumes).
- `Service` generated alongside it, with `ClusterIP`, `NodePort`, or `LoadBalancer` type.
- Volume support for `emptyDir`, `configMap`, `secret`, `persistentVolumeClaim`, and `csi`.
- Continuous reconciliation every 10 seconds, including cleanup when a `SimpleApp` is deleted.
- Orphan reaping: managed Deployments and Services that no longer have a matching `SimpleApp` are deleted.
- Ownership guardrails: objects not labelled as managed by SimpleApp are never modified or deleted.
- Self-healing spec: duplicate ports and volumes are removed and written back to the custom resource.

## How it works

1. On startup the controller reads its in-cluster configuration and discovers its own namespace from the
   service-account namespace file (`/var/run/secrets/kubernetes.io/serviceaccount/namespace`). It then verifies
   through the OpenAPI V3 endpoint that `apps.raulpedroche.es/v1alpha1` is served (i.e. that the CRD is installed).
2. Every 10 seconds it lists the `SimpleApp` resources in that namespace, along with all `Deployment`s and
   `Service`s carrying the label `app.kubernetes.io/managed-by=simpleapp`.
3. Each `SimpleApp` is reconciled: the controller creates the Deployment and Service if missing, or updates them
   when the generated specs differ from what is running.
4. `SimpleApp` resources that disappeared since the previous pass have their Deployment and Service deleted.
5. Any remaining managed Deployment or Service whose name does not match a live `SimpleApp` is treated as an
   orphan and deleted.

All generated objects are named after the `SimpleApp` and labelled with both
`app.kubernetes.io/managed-by=simpleapp` and `app=<name>`; the same labels are used for the Pod template and the
Service selector. If a Deployment or Service with the target name already exists but lacks the managed-by label,
the controller refuses to adopt it and logs an error instead.

The controller fails fast on listing errors: if the periodic API listing fails, the process exits and Kubernetes
restarts the controller pod. Errors affecting a single resource (create/update/delete failures) are logged and
retried on the next pass.

### Port mapping

Despite its name, `hostPort` in the `SimpleApp` spec is **not** applied as a container `hostPort`. It is used as
the **Service port**, while `containerPort` becomes the Service's `targetPort` and the container port:

| SimpleApp field | Deployment container | Service |
| --- | --- | --- |
| `containerPort` | `containerPort` | `targetPort` |
| `hostPort` | — | `port` |
| `protocol` | `protocol` | `protocol` |
| `name` | `name` | `name` (see below) |

Service port names are derived from `ports[].name`: a provided name is prefixed with `u-` and truncated to 13
characters, while an unnamed port in a multi-port spec gets an auto-generated
`a-<protocol>-<hostPort>-<containerPort>` name. Duplicate container ports are silently dropped when building the
Deployment.

Set port names explicitly and keep them short (15 characters or fewer): the raw `ports[].name` is used for the
container port, and a single unnamed port produces the Service port name `u-`, which Kubernetes rejects.

## The SimpleApp resource

```yaml
apiVersion: apps.raulpedroche.es/v1alpha1
kind: SimpleApp
metadata:
  name: sample
spec:
  image: nginx:latest
  replicas: 3
  serviceType: ClusterIP
  ports:
    - name: http
      hostPort: 80
      containerPort: 80
  env:
    - name: ENV_VAR
      value: "value"
  volumes:
    - mountPath: /usr/share/nginx/html
      configMap:
        name: sample-content
        defaultMode: 0o0644
```

A complete runnable example, including the mounted `ConfigMap`, is in [sample.yml](sample.yml).

### Spec reference

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `image` | string | — (required) | Container image to run. |
| `replicas` | integer | `1` | Desired number of pods. `0` is allowed. |
| `serviceType` | string | `ClusterIP` | One of `ClusterIP`, `NodePort`, `LoadBalancer`. |
| `ports` | list | — | Ports exposed by the Service and container. |
| `ports[].name` | string | — | Port name; auto-generated when omitted with multiple ports (15 characters max). |
| `ports[].hostPort` | integer | — (required) | Port exposed by the Service (`1`–`65535`). |
| `ports[].containerPort` | integer | — (required) | Port on the pod/container (`1`–`65535`). |
| `ports[].protocol` | string | `TCP` | One of `TCP`, `UDP`, `SCTP`. |
| `env` | list | — | Literal environment variables (`name` and `value` only). |
| `volumes` | list | — | Volumes mounted into the single container. |
| `volumes[].mountPath` | string | — (required) | Mount path inside the container. |
| `volumes[].emptyDir` | object | — | Temporary directory sharing the pod's lifetime (`medium`, `sizeLimit`). |
| `volumes[].configMap` | object | — | ConfigMap volume (`name`, `defaultMode`, `items`, `optional`). |
| `volumes[].secret` | object | — | Secret volume (`name`, `defaultMode`, `items`, `optional`). |
| `volumes[].persistentVolumeClaim` | object | — | PVC reference (`claimName`, `readOnly`). |
| `volumes[].csi` | object | — | CSI inline volume (`driver`, `fsType`, `readOnly`, `nodePublishSecretRef`, `volumeAttributes`). |

Each volume entry must specify `mountPath` and exactly one volume source; the CRD enforces this with a `oneOf`.
Environment variables support only literal `name`/`value` pairs — `valueFrom` references are not part of the schema.

## Quick start

### Prerequisites

- A Kubernetes cluster with the OpenAPI v3 endpoint (Kubernetes 1.24+). The CRD uses `apiextensions.k8s.io/v1`,
  and the controller is built against client-go v0.34.
- `kubectl` configured against the cluster.
- Docker, to build the controller image.
- Go 1.25.7+ only if you want to build the binary outside Docker.

### 1. Build the controller image

```sh
./build.sh
# equivalent to:
docker build -t simpleappcontroller:latest .
```

### 2. Make the image available to the cluster

The example manifest references `simpleappcontroller:latest` with `imagePullPolicy: Never`, so the image must
already be present on the node:

```sh
# kind
kind load docker-image simpleappcontroller:latest

# minikube
minikube image load simpleappcontroller:latest
```

For a remote cluster, push the image to a registry and update the image (and `imagePullPolicy`) in the
`simpleapp-controller` Deployment.

### 3. Install the CRD, RBAC, and controller

```sh
kubectl apply -f simpleapp.yml   # assumes the default namespace; see the note after cleanup
kubectl rollout status deployment/simpleapp-controller
```

### 4. Create a SimpleApp

```sh
kubectl apply -f sample.yml
```

### 5. Verify

```sh
kubectl get simpleapps,deployments,services,pods
kubectl port-forward svc/sample 8080:80
# in another terminal
curl http://localhost:8080
```

The controller logs (Deployment/Service creation and updates) are visible with:

```sh
kubectl logs deployment/simpleapp-controller
```

### 6. Clean up

```sh
kubectl delete -f sample.yml   # the controller removes the Deployment and Service within ~10s
sleep 15
kubectl delete -f simpleapp.yml
```

Cleanup of generated objects is performed by the running controller, so give it a pass before removing the
controller itself.

Note that `simpleapp.yml` pins the ServiceAccount, Role, and RoleBinding to the `default` namespace, while the
controller Deployment is created in whatever namespace the current `kubectl` context uses. Keep the context on
`default` for the steps above, or update all the namespace fields together. The controller only manages
`SimpleApp` resources in the namespace it runs in, so apply the CRs there.

## Using the published image

The [Docker workflow](.github/workflows/docker-publish.yml) builds and pushes the image to
`ghcr.io/pecio/simpleapp` on every push to `main`, on `v*.*.*` tags, and on a monthly schedule. Published images
are signed with cosign (keyless).

To use it instead of a locally built image:

```sh
kubectl set image deployment/simpleapp-controller \
  simpleapp-controller=ghcr.io/pecio/simpleapp:main
kubectl patch deployment simpleapp-controller --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
```

Replace `main` with a released version tag if you prefer a pinned build.

## Development

The project is a single Go module (`github.com/pecio/simpleapp`) using the Kubernetes client-go libraries
(`k8s.io/client-go` v0.34.0).

```sh
# with mise (uses the Go version in mise.toml)
mise install

# build
go build -o simpleappcontroller .

# vet
go vet ./...
```

The controller expects to run inside a Kubernetes pod: it requires in-cluster credentials and the mounted
service-account namespace. There are no test files yet, so `go test ./...` only compiles the packages.

The Dockerfile builds a static binary (`CGO_ENABLED=0`) with `golang:1.26.3` and copies it into a `scratch`
image, producing a very small controller image.

## Repository layout

| Path | Purpose |
| --- | --- |
| `main.go` | Startup, namespace discovery, and the reconciliation loop. |
| `simpleapp.go` | `SimpleApp` types and the logic that builds Deployments/Services and handles deletion. |
| `utils/compare.go` | Helpers for comparing generated objects with running ones. |
| `simpleapp.yml` | CRD, ServiceAccount, Role, RoleBinding, and the controller Deployment. |
| `sample.yml` | Example ConfigMap and SimpleApp (nginx serving a static page). |
| `Dockerfile`, `build.sh` | Container build. |
| `mise.toml` | Go toolchain version. |
| `.github/workflows/docker-publish.yml` | CI image publishing to GHCR. |

## Limitations

SimpleApp intentionally keeps a narrow scope. Consider these before using it for anything important:

- **Namespace-scoped:** it only reconciles `SimpleApp` resources in its own namespace; one controller instance
  per namespace is required.
- **Polling:** changes are observed every 10 seconds, not via watch/informers, so there is no immediate feedback;
  the CRD has no status subresource and the controller emits no events.
- **One container, no lifecycle settings:** no sidecars, init containers, probes, resource requests/limits, or
  image pull secrets for the generated pods.
- **No leader election:** run a single controller replica, as the example manifest does.
- **Fail-fast:** a failed resource listing terminates the process (Kubernetes restarts it), and startup fails if
  the CRD is not installed or the OpenAPI v3 endpoint is unavailable.
- **Mount path restrictions:** the CRD requires mount paths to be absolute and match
  `^(/(\.?[A-Za-z0-9_-][A-Za-z0-9._-]*))+$`, so segments may contain letters, digits, dashes, dots, and
  underscores, but they must not be empty or contain `..`.
- **Name-based association:** generated objects are matched to `SimpleApp`s by name (no owner references), so an
  unmanaged Deployment or Service with the same name blocks reconciliation.
