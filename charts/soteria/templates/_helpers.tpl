{{/*
Expand the name of the chart.
Truncated to 63 characters (Kubernetes name limit).
*/}}
{{- define "soteria.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
Uses release name + chart name, or fullnameOverride if set.
Truncated to 63 characters (Kubernetes name limit).
*/}}
{{- define "soteria.fullname" -}}
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
{{- define "soteria.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "soteria.labels" -}}
helm.sh/chart: {{ include "soteria.chart" . }}
{{ include "soteria.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels used for matching pods to services and deployments.
*/}}
{{- define "soteria.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soteria.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use.
Uses controller.serviceAccount.name if set, otherwise the fullname.
*/}}
{{- define "soteria.serviceAccountName" -}}
{{- if .Values.controller.serviceAccount.name }}
{{- .Values.controller.serviceAccount.name }}
{{- else }}
{{- include "soteria.fullname" . }}
{{- end }}
{{- end }}

{{/*
Resolve image tag: use the provided tag or fall back to .Chart.AppVersion.
Usage: {{ include "soteria.imageTag" (dict "tag" .Values.controller.image.tag "ctx" .) }}
*/}}
{{- define "soteria.imageTag" -}}
{{- .tag | default .ctx.Chart.AppVersion }}
{{- end }}
