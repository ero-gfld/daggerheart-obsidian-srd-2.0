# {{ .name }}

**_Tier {{ .tier }} {{ .type }}._** _{{ .description }}_

- **Motives & Tactics:** {{ .motives_and_tactics }}
- **Difficulty:** {{ .difficulty }} | **Thresholds:** {{ .thresholds }} | **HP:** {{ .hp }} | **Stress:** {{ .stress }}
- **ATK:** {{ .atk }} | **{{ .attack }}:** {{ .range }} | {{ .damage }}{{ if .experience }}
- **Experience:** {{ .experience }}{{ end }}

### {{ headingOr .features_heading "FEATURES" }}
{{- range featureGroups .feature }}

**_{{ featureLabel .feature.name }}_** {{ .feature.text }}
{{- if .children }}

{{ range $i, $c := .children }}{{ if $i }}>
{{ end }}{{ if eq $c.kind "attack" }}> **{{ $c.name }}:** {{ $c.text }}
{{ else }}> **_{{ featureLabel $c.name }}_** {{ $c.text }}
{{ end }}{{ end }}
{{- end }}
{{- end }}
