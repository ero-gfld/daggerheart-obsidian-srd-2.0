# Building

## 01_pdf

    pipx install --python python3.12 marker-pdf
    cd .build/01_pdf
    marker_single \
        --disable_ocr \
        --use_llm \
        --llm_service marker.services.openai.OpenAIService \
        --config_json marker_config.json \
        --openai_api_key "$OPENAI_API_KEY" \
        --openai_model gpt-5.1 \
        --timeout 180 \
        --max_concurrency 1 \
        --output_format markdown \
        --output_dir out \
        DH-SRD-2026-08-25.pdf
    cp out/DH-SRD-2026-08-25.md DH-SRD-2026-08-25.md
    npx --yes markdownlint-cli DH-SRD-2026-08-25.md

And then a lot of manual markdown cleanup...

## 02_csv, 03_json, 04_md

    go run .build/02_csv/extract_from_md.go
    go run .build/03_json/extract_from_csv.go
    go run .build/04_md/extract_from_json.go

## Testing Static Site

    pipx install mkdocs
    pipx inject mkdocs mkdocs-material
    mkdocs serve
