# DCM Helm Chart

AI agents maintaining this chart: see [AGENTS.md](AGENTS.md).

## Prerequisites

- Kubernetes 1.24+ or OpenShift 4.12+
- Helm 3.x
- A default StorageClass configured in the cluster (for PostgreSQL and NATS persistent volumes)

## Quick Start

Create the target namespace and switch into it, then create the database Secret
before install (lab defaults):

```bash
kubectl create namespace dcm
kubectl config set-context --current --namespace=dcm

kubectl create secret generic dcm-db \
  --from-literal=POSTGRES_USER=admin \
  --from-literal=POSTGRES_PASSWORD=adminpass \
  --from-literal=DB_USER=admin \
  --from-literal=DB_PASS=adminpass \
  --from-literal=DB_PASSWORD=adminpass
```

Install the control-plane stack into the current context namespace (`dcm`).
Enable the environment-agent (embedded SPs) as needed.

### OpenShift

```bash
helm install dcm deploy/helm/dcm \
  --set environmentAgent.enabled=true \
  --set environmentAgent.embeddedSps=container
```

OpenShift Routes are enabled by default for control-plane and DCM UI.

### Kubernetes

```bash
helm install dcm deploy/helm/dcm \
  --set controlPlane.route.enabled=false \
  --set dcmUi.route.enabled=false \
  --set environmentAgent.enabled=true \
  --set environmentAgent.embeddedSps=container
```

Access via port-forward:

```bash
kubectl port-forward svc/dcm-control-plane 8080:8080
kubectl port-forward svc/dcm-dcm-ui 7007:7007
```

Then open:
- Control-plane API: http://localhost:8080
- DCM UI: http://localhost:7007

## Enabling Environment Agent

Deploys the [environment-agent](https://github.com/dcm-project/environment-agent) with embedded
Service Providers in-process. Uses a chart-created ServiceAccount and workload RBAC.

With `auth.enabled=true`, registration and heartbeats use Keycloak client credentials
(`dcm-proxy` by default).

Environment Agent's embedded SP mapping to a backend platform :

| Embedded SP | Backend |
|---|---|
| `container` | Kubernetes Deployments\|Pods\|Services |
| `vm` | KubeVirt VirtualMachines |
| `storage` | Kubernetes PersistentVolumeClaims |
| `network` | Kubernetes Services |
| `cluster` | ACM/MCE HyperShift |

Escape commas in `--set` values (for example `container\,vm`). Workload namespaces default to
`default`. If you change `containerNamespace`, `vmNamespace`, `storageNamespace`,
`networkNamespace`, or `clusterNamespace`, create those namespaces before install/upgrade.

```bash
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set environmentAgent.enabled=true \
  --set environmentAgent.embeddedSps=container
```

To include `container` and `vm`:

```bash
helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set environmentAgent.enabled=true \
  --set environmentAgent.embeddedSps=container\,vm \
  --set environmentAgent.externalSvcType=LoadBalancer
```

When `embeddedSps` includes:

1.  `vm`, ensure KubeVirt or CNV is installed and available on the cluster.
2.  `container`, set `environmentAgent.externalSvcType=NodePort` on Kind.
3.  `network`, workloads use `environmentAgent.networkNamespace`.
4.  `storage`, workloads use `environmentAgent.storageNamespace`. 
5.  `cluster`, create a pull-secret Secret and set `environmentAgent.pullSecretRef`:

```bash
PULL_SECRET=$(oc get secret pull-secret -n openshift-config -o jsonpath='{.data.\.dockerconfigjson}')
kubectl create secret generic dcm-acm-pull-secret \
  --from-literal=pull-secret="$PULL_SECRET"

helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set environmentAgent.enabled=true \
  --set environmentAgent.embeddedSps=container\,cluster \
  --set environmentAgent.pullSecretRef=dcm-acm-pull-secret \
  --set environmentAgent.clusterNamespace=clusters \
  --set environmentAgent.baseDomain=example.com
```

Agent API is ClusterIP. Port-forward to verify:

```bash
kubectl port-forward svc/dcm-environment-agent 8081:8080
curl http://127.0.0.1:8081/api/v1alpha1/health
```

## Values schema and maintainability

**Install-time validation**: `deploy/helm/dcm/values.schema.json` is the contract that helm validates against at install/upgrade/lint time.

**Keep in sync:** When modifying `values.yaml`, also update `values.schema.json` to catch mistypes or unknown values early. Running `make helm-chart-sync` then `make helm-chart-check` ensures your changes don't introduce lint errors.

**Template-time rules**: `scripts/verify-template.sh` remains authoritative for render-time constraints that the schema cannot express (e.g., "Secret must not emit when authSecretRef is set"). Schema guards and template guards operate independently.

### Run full validation

```bash
make helm-chart-sync   # realm file
make helm-chart-check  # verify/schema/lint/template
scripts/verify-template.sh deploy/helm/dcm    # positive & negative tests
```

## Authentication

Authentication mirrors the Compose stack: Keycloak as the IdP, and the control-plane
validates JWT bearer tokens (primary) or proxy headers (fallback). Route/Ingress
target the control-plane Service.

Auth is **disabled by default** (`auth.enabled=false`). Enabling it deploys Keycloak
and sets `AUTH_DISABLED=false` on the control-plane.

**Credentials** (required when auth is enabled): create a pre-existing Secret **in the
release namespace** with keys `KEYCLOAK_ADMIN`, `KEYCLOAK_ADMIN_PASSWORD`,
`DCM_DEV_USER_PASSWORD`, and `AUTH_PROXY_SECRET`, then set `auth.authSecretRef` (default
`dcm-auth`). The chart does not render credential Secrets.

> **Credential rotation:** `--import-realm` only imports a new realm. Updating
> `AUTH_PROXY_SECRET` or `DCM_DEV_USER_PASSWORD` and restarting Keycloak does not change credentials already stored in Keycloak. Rotate the corresponding client or user through the Keycloak admin UI/API, then restart the control-plane to load the new Secret.

### Enable authentication (lab)

```bash
kubectl create secret generic dcm-auth \
  --from-literal=KEYCLOAK_ADMIN=admin \
  --from-literal=KEYCLOAK_ADMIN_PASSWORD=admin \
  --from-literal=DCM_DEV_USER_PASSWORD=admin \
  --from-literal=AUTH_PROXY_SECRET=dcm-dev-proxy-secret

helm upgrade dcm deploy/helm/dcm --reuse-values \
  --set auth.enabled=true
```

Or on install:

```bash
helm install dcm deploy/helm/dcm \
  --set auth.enabled=true
```

(`auth.authSecretRef` defaults to `dcm-auth`; create the Secret in the release namespace first.)

The realm source is `deploy/keycloak/realm-export.json` (same file Compose uses). The
chart bundles a copy under `files/`; after editing the source, run `make helm-chart-sync`
and commit both files. `make helm-chart-verify-sync` catches drift; CI runs
`make helm-chart-check` (verify, lint, template).

When `auth.enabled=true` with the environment-agent, the chart sets `DCM_AUTH_*` using
the Keycloak `dcm-proxy` client (`AUTH_PROXY_SECRET` in `auth.authSecretRef`). Override
the client with `environmentAgent.authClientID` if needed.

### Auth values

| Value | Default | Description |
|---|---|---|
| `auth.enabled` | `false` | Deploy Keycloak and enable control-plane auth |
| `auth.authSecretRef` | `dcm-auth` | Pre-existing Secret for auth credentials (required when auth enabled) |
| `auth.jwtAudience` | `dcm-api` | Expected JWT `aud`; empty skips audience check |
| `auth.issuerURL` | _(empty)_ | OIDC issuer; empty uses `http://<fullname>-keycloak:8080/realms/dcm` |
| `auth.keycloak.image` | `quay.io/keycloak/keycloak:26.0.1` | Keycloak image |
| `auth.keycloak.route.enabled` | `false` | Optional OpenShift Route for the Keycloak admin console |
| `auth.keycloak.ingress.enabled` | `false` | Optional Ingress for the Keycloak admin console |

When exposing Keycloak via Route/Ingress, set `auth.issuerURL` to the external issuer
(for example `https://keycloak.apps.example.com/realms/dcm`) so token `iss` and
`KC_HOSTNAME` stay aligned.

### Access Keycloak

With the default ClusterIP Service, port-forward the Keycloak Service
(`<fullname>-keycloak`, e.g. `dcm-keycloak` for release `dcm`):

```bash
kubectl port-forward svc/dcm-keycloak 8180:8080
```

Then open http://localhost:8180 (admin / admin). The realm `dcm` includes clients
`dcm-proxy` (confidential; secret from `AUTH_PROXY_SECRET` in `dcm-auth`) and `dcm-cli`,
and user `dcm-admin` / `admin`.

### Call the API with a JWT

Lab-only password grant against the confidential `dcm-proxy` client. The password
grant is deprecated in OAuth 2.1; use it here only for local token acquisition,
not as a production pattern. Port-forward Keycloak and the control-plane first
(`svc/dcm-keycloak 8180:8080`, `svc/dcm-control-plane 8080:8080`):

```bash
TOKEN=$(curl -s -X POST 'http://localhost:8180/realms/dcm/protocol/openid-connect/token' \
  -d 'grant_type=password' \
  -d 'client_id=dcm-proxy' \
  -d 'client_secret=dcm-dev-proxy-secret' \
  -d 'username=dcm-admin' \
  -d 'password=admin' | jq -r .access_token)

curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

`/api/v1alpha1/health` remains unauthenticated.

## Uninstall

```bash
helm uninstall dcm
```

Note: PersistentVolumeClaims for PostgreSQL, NATS, and the environment-agent are not deleted automatically. To remove them:

```bash
kubectl delete pvc -l app.kubernetes.io/instance=dcm
```
