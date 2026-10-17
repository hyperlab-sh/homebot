{{- range . }}
-------------------------------------------------------------------------------
{{ .Name }} {{ .Version }} ({{ .LicenseName }})
{{ .LicenseURL }}

{{ if .LicenseText }}{{ .LicenseText }}{{ else }}MISSING_LICENSE_TEXT: {{ .Name }}{{ end }}
{{- end }}
