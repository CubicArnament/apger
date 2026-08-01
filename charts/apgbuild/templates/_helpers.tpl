{{- define "apgbuild.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "apgbuild.fullname" -}}
{{- if .Values.fullnameOverride }}{{ .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}{{ else }}{{ printf "%s-%s" .Release.Name (include "apgbuild.name" .) | trunc 63 | trimSuffix "-" }}{{ end }}
{{- end }}

{{- define "apgbuild.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "apgbuild.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "apgbuild.workspacePVC" -}}
{{- if .Values.workspace.create }}{{ include "apgbuild.fullname" . }}-workspace{{ else }}{{ required "workspace.existingClaim is required when create=false" .Values.workspace.existingClaim }}{{ end }}
{{- end }}
