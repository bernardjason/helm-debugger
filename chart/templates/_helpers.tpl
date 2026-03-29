{{- define "test-chart.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "test-chart.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "test-chart.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "test-chart.labels" -}}
app.kubernetes.io/name: {{ include "test-chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- with .Values.labels }}
{{ toYaml . }}
{{- end }}
{{- end -}}



{{- define "helm-command-lab.name" -}}
.Chart.Name
{{- end -}}

{{- define "helm-command-lab.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "helm-command-lab.image" -}}
{{- $repo := .Values.image.repository -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" $repo $tag -}}
{{- end -}}

{{- define "helm-command-lab.labels" -}}
app.kubernetes.io/name: {{ include "helm-command-lab.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
{{- end -}}

{{- define "helm-command-lab.fail-demo" -}}
{{- if .Values.neverTrueForFail }}
{{ fail "intentional fail demo" }}
{{- else }}
fail-disabled
{{- end }}
{{- end -}}
