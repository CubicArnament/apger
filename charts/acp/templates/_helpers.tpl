{{- define "acp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "acp.fullname" -}}
{{- if .Values.fullnameOverride }}{{ .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{ else }}{{ printf "%s-%s" .Release.Name (include "acp.name" .) | trunc 63 | trimSuffix "-" }}{{ end }}
{{- end }}

{{- define "acp.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "acp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "acp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "acp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
