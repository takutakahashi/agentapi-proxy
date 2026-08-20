{{- define "session-manager.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- define "session-manager.fullname" -}}
{{- if .Values.fullnameOverride }}{{ .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{ else }}{{ include "session-manager.name" . }}{{ end }}
{{- end }}
{{- define "session-manager.labels" -}}
app.kubernetes.io/name: {{ include "session-manager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: session-manager
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}
{{- define "session-manager.selectorLabels" -}}
app.kubernetes.io/name: {{ include "session-manager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: session-manager
{{- end }}
{{- define "session-manager.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}{{ default (include "session-manager.fullname" .) .Values.serviceAccount.name }}{{ else }}{{ required "serviceAccount.name is required when create=false" .Values.serviceAccount.name }}{{ end }}
{{- end }}
{{- define "session-manager.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}
