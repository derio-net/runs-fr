{{/*
Expand the name of the chart.
*/}}
{{- define "runs-fr.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name. Truncated at 63 chars for the DNS
label limit that some Kubernetes name fields impose.
*/}}
{{- define "runs-fr.fullname" -}}
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
{{- define "runs-fr.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "runs-fr.labels" -}}
helm.sh/chart: {{ include "runs-fr.chart" . }}
{{ include "runs-fr.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "runs-fr.selectorLabels" -}}
app.kubernetes.io/name: {{ include "runs-fr.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
The name of the ServiceAccount to use.
*/}}
{{- define "runs-fr.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "runs-fr.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
The namespace the gateway watches for run pods (and where the pods/exec Role
lives). Defaults to the release namespace when .Values.namespace is empty.
*/}}
{{- define "runs-fr.watchNamespace" -}}
{{- default .Release.Namespace .Values.namespace }}
{{- end }}
