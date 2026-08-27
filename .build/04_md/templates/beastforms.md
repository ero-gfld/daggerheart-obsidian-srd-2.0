# {{ .name }}

**_Tier {{ .tier }}_** _{{ .examples }}_

{{ if .trait_bonus }}- **Trait Bonus:** {{ .trait_bonus }}
{{ end }}{{ if .evasion_bonus }}- **Evasion Bonus:** {{ .evasion_bonus }}
{{ end }}{{ if .attack }}- **Attack:** {{ .attack }}
{{ end }}{{ if .advantages }}- **Advantages:** {{ .advantages }}
{{ end }}
### FEATURES

{{- range .feature }}

**_{{ featureLabel .name }}_** {{ .text }}
{{- end }}
