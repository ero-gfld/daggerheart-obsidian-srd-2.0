# {{ .name }}

{{ .description }}

### {{ literalHeading .features_heading "ANCESTRY FEATURE" (len .feature) }}

{{- range .feature }}

**_{{ featureLabel .name }}_** {{ .text }}
{{- end }}
