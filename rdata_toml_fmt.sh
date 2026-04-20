#!/usr/bin/env bash
# rdata_toml_fmt.sh — rename repodata TOML files to pkg-name_arch_version.toml

set -e

REPODATA="${1:-repodata}"

if [ ! -d "$REPODATA" ]; then
    echo "Error: directory '$REPODATA' not found"
    exit 1
fi

renamed=0
skipped=0
errors=0

while IFS= read -r -d '' file; do
    dir=$(dirname "$file")
    base=$(basename "$file" .toml)

    # Extract fields from [package] section
    name=$(grep -m1 '^name\s*=' "$file" | sed 's/.*=\s*"\(.*\)"/\1/')
    version=$(grep -m1 '^version\s*=' "$file" | sed 's/.*=\s*"\(.*\)"/\1/')
    arch=$(grep -m1 '^architecture\s*=' "$file" | sed 's/.*=\s*"\(.*\)"/\1/')

    if [ -z "$name" ] || [ -z "$version" ] || [ -z "$arch" ]; then
        echo "SKIP  $file (missing name/version/architecture)"
        skipped=$((skipped + 1))
        continue
    fi

    new_name="${name}_${arch}_${version}.toml"
    new_path="$dir/$new_name"

    if [ "$file" = "$new_path" ]; then
        skipped=$((skipped + 1))
        continue
    fi

    if [ -e "$new_path" ]; then
        echo "ERROR $file -> $new_name (target exists)"
        errors=$((errors + 1))
        continue
    fi

    mv "$file" "$new_path"
    echo "OK    $(basename "$file") -> $new_name"
    renamed=$((renamed + 1))

done < <(find "$REPODATA" -name "*.toml" -print0)

echo ""
echo "Done: $renamed renamed, $skipped skipped, $errors errors"

if [ "$renamed" -gt 0 ]; then
    git add "$REPODATA"
    git commit -m "chore: formatted $renamed packages, skipped $skipped, errors $errors"
    git push origin main
    echo -e "\nPushed to origin/main"
fi
