{{/*
Expand the name of the chart.
*/}}
{{- define "agentapi-proxy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "agentapi-proxy.fullname" -}}
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
{{- define "agentapi-proxy.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "agentapi-proxy.labels" -}}
helm.sh/chart: {{ include "agentapi-proxy.chart" . }}
{{ include "agentapi-proxy.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "agentapi-proxy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentapi-proxy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "agentapi-proxy.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "agentapi-proxy.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
scia OAuth broker resource names.
*/}}
{{- define "agentapi-proxy.sciaName" -}}
{{- printf "%s-scia-oauth" (include "agentapi-proxy.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "agentapi-proxy.sciaServiceName" -}}
scia-oauth
{{- end }}

{{- define "agentapi-proxy.sciaSecretName" -}}
{{- $scia := .Values.scia | default dict }}
{{- $oauth := $scia.oauth | default dict }}
{{- $google := $oauth.google | default dict }}
{{- $secret := $google.secret | default dict }}
{{- default (include "agentapi-proxy.sciaName" .) $secret.existingSecret }}
{{- end }}

{{- define "agentapi-proxy.sciaTodoistSecretName" -}}
{{- $scia := .Values.scia | default dict }}
{{- $oauth := $scia.oauth | default dict }}
{{- $todoist := $oauth.todoist | default dict }}
{{- $secret := $todoist.secret | default dict }}
{{- default (printf "%s-todoist" (include "agentapi-proxy.sciaName" .) | trunc 63 | trimSuffix "-") $secret.existingSecret }}
{{- end }}

{{- define "agentapi-proxy.sciaNotionSecretName" -}}
{{- $scia := .Values.scia | default dict }}
{{- $oauth := $scia.oauth | default dict }}
{{- $notion := $oauth.notion | default dict }}
{{- $secret := $notion.secret | default dict }}
{{- default (printf "%s-notion" (include "agentapi-proxy.sciaName" .) | trunc 63 | trimSuffix "-") $secret.existingSecret }}
{{- end }}
