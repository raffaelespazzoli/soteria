{{- /*
Validation template — included by deployment.yaml to fail fast on
misconfigured values. Emits no YAML output; uses required/fail only.
*/ -}}
{{- define "soteria.validate" -}}

{{- /* Site configuration */ -}}
{{- required "site.name is required" .Values.site.name | quote | trunc 0 -}}
{{- if not (has .Values.site.role (list "seed" "joining")) -}}
{{- fail "site.role must be 'seed' or 'joining'" -}}
{{- end -}}

{{- /* TLS issuer */ -}}
{{- required "tls.issuerRef.name is required" .Values.tls.issuerRef.name | quote | trunc 0 -}}

{{- /* UI mode */ -}}
{{- if not (has .Values.ui.mode (list "console-plugin" "standalone" "none")) -}}
{{- fail "ui.mode must be 'console-plugin', 'standalone', or 'none'" -}}
{{- end -}}

{{- /* ScyllaDB external mode guards */ -}}
{{- if eq .Values.scylladb.mode "external" -}}
{{- required "scylladb.external.contactPoints is required when scylladb.mode=external" .Values.scylladb.external.contactPoints | quote | trunc 0 -}}
{{- if .Values.scylladb.external.tls.enabled -}}
{{- required "scylladb.external.tls.secretName is required when scylladb.external.tls.enabled=true" .Values.scylladb.external.tls.secretName | quote | trunc 0 -}}
{{- end -}}
{{- end -}}

{{- end -}}
