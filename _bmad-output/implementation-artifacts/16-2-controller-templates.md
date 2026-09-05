# Story 16.2: Controller Templates

Status: ready-for-dev

## Story

As a platform engineer,
I want Helm templates for the Soteria controller (aggregated API server + reconciler),
so that the controller and its supporting resources are deployed and configured via Helm values.

## Acceptance Criteria

**AC1: Controller Deployment renders correctly**
Given a valid `values.yaml`
When I run `helm template`
Then a Deployment is rendered with container args wired from values (site-name, leader-elect, apiserver port) and ScyllaDB connection args conditional on `scylladb.mode`

**AC2: ServiceAccount is created**
Given the chart is rendered
When I inspect the output
Then a ServiceAccount named using the `soteria.serviceAccountName` helper is present

**AC3: RBAC resources match existing manifests**
Given the chart is rendered
When I compare the ClusterRole and ClusterRoleBinding to `config/rbac/`
Then the rules are equivalent to the existing kustomize-generated RBAC manifests

**AC4: APIService is configured with cert-manager injection**
Given the chart is rendered
When I inspect the APIService resource
Then it registers `v1alpha1.soteria.io`, has the `cert-manager.io/inject-ca-from` annotation, and references the correct Service

**AC5: ValidatingWebhookConfiguration has CA injection**
Given the chart is rendered
When I inspect the ValidatingWebhookConfiguration
Then it has the `cert-manager.io/inject-ca-from` annotation referencing the webhook Certificate

**AC6: Services expose correct ports**
Given the chart is rendered
When I inspect the Service resources
Then the apiserver Service listens on 6443, the webhook Service maps 443→9443, and the metrics Service listens on 8443

**AC7: cert-manager Certificates reference tls.issuerRef**
Given `tls.issuerRef.name` and `tls.issuerRef.kind` are set in values
When I render the chart
Then the apiserver-serving, webhook-serving, and metrics-serving Certificate CRs all reference the configured issuer

**AC8: Managed ScyllaDB connection wiring**
Given `scylladb.mode=managed`
When I render the controller Deployment
Then contact points are derived from the in-cluster ScyllaDB service name and mTLS secrets are mounted as volumes

**AC9: External ScyllaDB connection wiring**
Given `scylladb.mode=external`
When I render the controller Deployment
Then the container args use `external.contactPoints` and TLS Secret is conditionally mounted from `external.tls.secretName`

## Tasks / Subtasks

- [ ] Task 1: Create Deployment template (AC: 1, 8, 9)
  - [ ] 1.1: Create `templates/controller/deployment.yaml` mirroring `config/manager/manager.yaml` structure — securityContext, probes, ports
  - [ ] 1.2: Wire base container args: `--leader-elect`, `--health-probe-bind-address=:8081`, `--site-name`, `--secure-port`
  - [ ] 1.3: Add conditional ScyllaDB args block for `scylladb.mode=managed` — contact points derived from in-cluster service name, mTLS volume mounts
  - [ ] 1.4: Add conditional ScyllaDB args block for `scylladb.mode=external` — contact points from `external.contactPoints`, conditional TLS volume mount
- [ ] Task 2: Create ServiceAccount template (AC: 2)
  - [ ] 2.1: Create `templates/controller/serviceaccount.yaml` using `soteria.serviceAccountName` helper
- [ ] Task 3: Create RBAC templates (AC: 3)
  - [ ] 3.1: Create `templates/controller/clusterrole.yaml` mirroring all rules from `config/rbac/role.yaml`
  - [ ] 3.2: Create `templates/controller/clusterrolebinding.yaml` binding to the ServiceAccount
- [ ] Task 4: Create APIService template (AC: 4)
  - [ ] 4.1: Create `templates/controller/apiservice.yaml` registering `v1alpha1.soteria.io` with `cert-manager.io/inject-ca-from` annotation pointing to apiserver Certificate
- [ ] Task 5: Create Webhook template (AC: 5)
  - [ ] 5.1: Create `templates/controller/webhookconfiguration.yaml` mirroring `config/webhook/manifests.yaml` with `cert-manager.io/inject-ca-from` annotation pointing to webhook Certificate
- [ ] Task 6: Create Service templates (AC: 6)
  - [ ] 6.1: Create `templates/controller/service-apiserver.yaml` — port 443→6443 (matching `config/apiservice/service.yaml`)
  - [ ] 6.2: Create `templates/controller/service-webhook.yaml` — port 443→9443 (matching `config/webhook/service.yaml`)
  - [ ] 6.3: Create `templates/controller/service-metrics.yaml` — port 8443
- [ ] Task 7: Create Certificate templates (AC: 7)
  - [ ] 7.1: Create `templates/controller/certificate-apiserver.yaml` referencing `tls.issuerRef` (based on `config/certmanager/ca-certificate.yaml` pattern)
  - [ ] 7.2: Create `templates/controller/certificate-webhook.yaml` referencing `tls.issuerRef` (based on `config/certmanager/webhook-serving-cert.yaml`)
  - [ ] 7.3: Create `templates/controller/certificate-metrics.yaml` referencing `tls.issuerRef` (based on `config/certmanager/metrics-serving-cert.yaml`)
- [ ] Task 8: Add volume/volumeMount logic (AC: 8, 9)
  - [ ] 8.1: Add apiserver-tls volume mount (always present) — `secretName: <release>-apiserver-tls`
  - [ ] 8.2: Add managed ScyllaDB mTLS volume mounts: `scylladb-client-tls` Secret at `/etc/soteria/scylladb-client-tls`
  - [ ] 8.3: Add external ScyllaDB TLS volume mount: conditional on `external.tls.enabled`, Secret at `/etc/soteria/scylladb-tls`
- [ ] Task 9: Verify rendered output (AC: 1–9)
  - [ ] 9.1: Run `helm template` with `scylladb.mode=managed` and verify all resources
  - [ ] 9.2: Run `helm template` with `scylladb.mode=external` and verify controller args switch

## Dev Notes

### Implementation Pattern

The Deployment template should mirror `config/manager/manager.yaml`. Key elements:

```yaml
# From config/manager/manager.yaml
spec:
  containers:
  - command: ["/manager"]
    args:
      - --leader-elect
      - --health-probe-bind-address=:8081
      - --site-name=$(SITE_NAME)
    ports:
      - containerPort: 6443
        name: apiserver
```

The kustomize overlay `config/default/kustomization.yaml` shows the full resource composition: `../rbac`, `../manager`, `../webhook`, `../certmanager`, `../apiservice`, `../scylladb`, plus patches for metrics, webhook, and cert injection.

The RBAC ClusterRole in `config/rbac/role.yaml` has rules for: namespaces, PVs, PVCs, secrets, events, cephblockpools, virtualmachines, volumereplications, volumegroupreplications, drplans, drexecutions, shadowpvs, storageclasses.

For cert-manager CA injection, `config/default/kustomization.yaml` uses kustomize replacements to wire cert-manager annotations. In Helm, use direct annotation references:
```yaml
annotations:
  cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/{{ include "soteria.fullname" . }}-apiserver-serving
```

The existing volume mount pattern from `hack/overlays/base/manager-scylladb-patch.yaml`:
```yaml
volumeMounts:
  - name: apiserver-tls
    mountPath: /etc/soteria/apiserver-tls
  - name: scylladb-client-tls
    mountPath: /etc/soteria/scylladb-client-tls
volumes:
  - name: apiserver-tls
    secret:
      secretName: soteria-apiserver-tls
  - name: scylladb-client-tls
    secret:
      secretName: soteria-scylladb-client-tls
```

### File Inventory

| File | Action | Description |
|------|--------|-------------|
| `charts/soteria/templates/controller/deployment.yaml` | NEW | Controller Deployment with conditional ScyllaDB args and volume mounts |
| `charts/soteria/templates/controller/serviceaccount.yaml` | NEW | ServiceAccount using helper |
| `charts/soteria/templates/controller/clusterrole.yaml` | NEW | ClusterRole mirroring `config/rbac/role.yaml` |
| `charts/soteria/templates/controller/clusterrolebinding.yaml` | NEW | ClusterRoleBinding |
| `charts/soteria/templates/controller/apiservice.yaml` | NEW | APIService for `v1alpha1.soteria.io` with CA injection |
| `charts/soteria/templates/controller/webhookconfiguration.yaml` | NEW | ValidatingWebhookConfiguration with CA injection |
| `charts/soteria/templates/controller/service-apiserver.yaml` | NEW | Service: 443→6443 |
| `charts/soteria/templates/controller/service-webhook.yaml` | NEW | Service: 443→9443 |
| `charts/soteria/templates/controller/service-metrics.yaml` | NEW | Service: 8443 |
| `charts/soteria/templates/controller/certificate-apiserver.yaml` | NEW | cert-manager Certificate for apiserver TLS |
| `charts/soteria/templates/controller/certificate-webhook.yaml` | NEW | cert-manager Certificate for webhook TLS |
| `charts/soteria/templates/controller/certificate-metrics.yaml` | NEW | cert-manager Certificate for metrics TLS |

### Key Constraints

- The Deployment `securityContext` must match `config/manager/manager.yaml`: `runAsNonRoot: true`, `seccompProfile.type: RuntimeDefault`, container with `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, drop `ALL` capabilities
- APIService name must be exactly `v1alpha1.soteria.io` (not prefixed by release name)
- The webhook validates `kubevirt.io/v1/virtualmachines` on CREATE and UPDATE (see `config/webhook/manifests.yaml`)
- Certificates: apiserver-serving uses scylladb-client-cert pattern; webhook and metrics use their respective cert patterns from `config/certmanager/`
- Depends on: Story 16.1 (chart skeleton and values.yaml)

### Project Structure Notes

- All controller templates go under `charts/soteria/templates/controller/` subdirectory
- RBAC rules are a direct copy from generated `config/rbac/role.yaml` — keep them in sync

### References

- [Source: `config/default/kustomization.yaml` — full kustomize overlay structure with all resource refs and replacement rules]
- [Source: `config/manager/manager.yaml` — controller Deployment spec with args, probes, ports, security context]
- [Source: `config/rbac/role.yaml` — complete RBAC rules for all API groups]
- [Source: `config/apiservice/apiservice.yaml` — APIService registration for `v1alpha1.soteria.io`]
- [Source: `config/apiservice/service.yaml` — apiserver Service (443→6443)]
- [Source: `config/webhook/manifests.yaml` — ValidatingWebhookConfiguration for VirtualMachine]
- [Source: `config/webhook/service.yaml` — webhook Service (443→9443)]
- [Source: `config/certmanager/webhook-serving-cert.yaml` — webhook Certificate pattern]
- [Source: `config/certmanager/metrics-serving-cert.yaml` — metrics Certificate pattern]
- [Source: `config/certmanager/scylladb-client-cert.yaml` — ScyllaDB client Certificate pattern]
- [Source: `hack/overlays/base/manager-scylladb-patch.yaml` — volume mounts for apiserver-tls and scylladb-client-tls]
- [Source: `hack/overlays/base/manager-args-patch.yaml` — full set of ScyllaDB and apiserver flags]
- [Source: `pkg/apiserver/options.go` — all `--scylladb-*` command-line flags]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
