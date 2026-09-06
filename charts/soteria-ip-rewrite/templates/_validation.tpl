{{- /*
Validation template — included by deployment.yaml to fail fast on
misconfigured values. Emits no YAML output; uses required/fail only.
*/ -}}
{{- define "soteria-ip-rewrite.validate" -}}

{{- /* TLS issuer — either createSelfSigned or explicit issuerRef.name */ -}}
{{- if and (not .Values.tls.createSelfSigned) (not .Values.tls.issuerRef.name) -}}
{{- fail "tls.issuerRef.name is required when tls.createSelfSigned is false" -}}
{{- end -}}

{{- end -}}
