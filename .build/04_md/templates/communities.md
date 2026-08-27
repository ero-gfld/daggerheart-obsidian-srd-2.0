# {{ .name }}

{{ .description }}

_{{ .note }}_

### {{ literalHeading .features_heading "COMMUNITY FEATURE" (len .feature) }}

{{- range .feature }}

**_{{ featureLabel .name }}_** {{ .text }}
{{- end }}
