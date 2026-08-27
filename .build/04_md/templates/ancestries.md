# {{ .name }}
{{- if .parent_group }}

_One of the [{{ .parent_group }}](../README.md#{{ .parent_group_anchor }}) ancestries._
{{- end }}

{{ .description }}

### {{ literalHeading .features_heading "ANCESTRY FEATURE" (len .feature) }}

{{- range .feature }}

**_{{ .name }}:_** {{ .text }}
{{- end }}
