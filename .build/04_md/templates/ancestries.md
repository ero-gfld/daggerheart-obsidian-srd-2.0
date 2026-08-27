# {{ .name }}

{{ .description }}

### {{ literalHeading .features_heading "ANCESTRY FEATURE" (len .feature) }}

{{- range .feature }}

**_{{ .name }}:_** {{ .text }}
{{- end }}
