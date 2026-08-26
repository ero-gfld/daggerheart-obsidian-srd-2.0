# {{ .name }}

**_Tier {{ .tier }} {{ .type }}._** _{{ .description }}_

- **Impulses:** {{ .impulses }}
- **Difficulty:** {{ .difficulty }}
- **Potential Adversaries:** {{ .potential_adversaries }}

### FEATURES

{{- range .feature }}

**_{{ featureLabel .name }}_** {{ .text }}{{ if .question }} _{{ .question }}_{{ end }}
{{- end }}
