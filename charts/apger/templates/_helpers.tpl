{{- define "apger.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "apger.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "apger.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "apger.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "apger.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "apger.selectorLabels" -}}
app.kubernetes.io/name: {{ include "apger.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "apger.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}{{ default (include "apger.fullname" .) .Values.serviceAccount.name }}{{ else }}{{ required "serviceAccount.name is required when create=false" .Values.serviceAccount.name }}{{ end }}
{{- end }}

{{- define "apger.outputPVC" -}}
{{- if .Values.persistence.output.create }}{{ include "apger.fullname" . }}-output{{ else }}{{ required "persistence.output.existingClaim is required when create=false" .Values.persistence.output.existingClaim }}{{ end }}
{{- end }}

{{- define "apger.cachePVC" -}}
{{- if .Values.persistence.cache.create }}{{ include "apger.fullname" . }}-cache{{ else }}{{ .Values.persistence.cache.existingClaim }}{{ end }}
{{- end }}
