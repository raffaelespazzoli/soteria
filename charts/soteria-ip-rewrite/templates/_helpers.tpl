{{/*
Expand the name of the chart.
Truncated to 63 characters (Kubernetes name limit).
*/}}
{{- define "soteria-ip-rewrite.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Uses release name + chart name, or fullnameOverride if set.
Truncated to 63 characters (Kubernetes name limit).
*/}}
{{- define "soteria-ip-rewrite.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soteria-ip-rewrite.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "soteria-ip-rewrite.labels" -}}
helm.sh/chart: {{ include "soteria-ip-rewrite.chart" . }}
{{ include "soteria-ip-rewrite.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels used for matching pods to services and deployments.
*/}}
{{- define "soteria-ip-rewrite.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soteria-ip-rewrite.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use.
Falls back to the fullname when not overridden.
*/}}
{{- define "soteria-ip-rewrite.serviceAccountName" -}}
{{- include "soteria-ip-rewrite.fullname" . }}
{{- end }}

{{/*
Resolve image tag: use the provided tag or fall back to .Chart.AppVersion.
Usage: {{ include "soteria-ip-rewrite.imageTag" (dict "tag" .Values.webhook.image.tag "ctx" .) }}
*/}}
{{- define "soteria-ip-rewrite.imageTag" -}}
{{- .tag | default .ctx.Chart.AppVersion }}
{{- end }}
