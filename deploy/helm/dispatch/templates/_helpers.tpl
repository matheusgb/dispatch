{{/*
Nome completo da imagem de um workload: <repository>-<workload>:<tag>.
Mesmo padrão que scripts/kind-deploy.sh já constrói e carrega no kind.
*/}}
{{- define "dispatch.image" -}}
{{ .root.Values.image.repository }}-{{ .name }}:{{ .root.Values.image.tag }}
{{- end -}}

{{/*
Labels comuns a todo objeto do chart.
*/}}
{{- define "dispatch.labels" -}}
app.kubernetes.io/part-of: dispatch
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}
