#!/usr/bin/env bash
# scripts/configure.sh
#
# Reads `pulumi stack output` and injects the values into the
# <script id="pulumi-config"> block inside index.html so the
# search page always reflects the current deployment — no manual
# copy-paste required.
#
# Usage (run from the repo root after `pulumi up`):
#   ./scripts/configure.sh
#   ./scripts/configure.sh --stack dev   # explicit stack name

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INDEX_HTML="$REPO_ROOT/index.html"

# ── Parse optional --stack flag ────────────────────────────────
STACK_FLAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --stack|-s) STACK_FLAG="--stack $2"; shift 2 ;;
    *) shift ;;
  esac
done

# ── Read Pulumi outputs ────────────────────────────────────────
echo "→ Reading Pulumi stack outputs…"
PULUMI_JSON=$(pulumi stack output --json $STACK_FLAG 2>/dev/null) || {
  echo "✗  'pulumi stack output' failed. Is the stack deployed?" >&2
  exit 1
}

OPENSEARCH_ENDPOINT=$(echo "$PULUMI_JSON" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['opensearchEndpoint']+'/images/_search')")
# s3BucketUrl is exported by main.go; fall back to constructing it from
# bucketName for stacks deployed before that export was added.
S3_BUCKET_URL=$(echo "$PULUMI_JSON" | python3 -c "
import sys, json
d = json.load(sys.stdin)
if 's3BucketUrl' in d:
    print(d['s3BucketUrl'])
else:
    bucket = d['bucketName']
    # Derive region from the endpoint URL (heuristic: eu-central-1 default)
    print(f'https://{bucket}.s3.eu-central-1.amazonaws.com/')
")

echo "   opensearchUrl : $OPENSEARCH_ENDPOINT"
echo "   s3BucketUrl   : $S3_BUCKET_URL"

# ── Inject into the <script id="pulumi-config"> block ─────────
# Replace everything between the opening <script> and </script>
# tags whose id is "pulumi-config".
python3 - "$INDEX_HTML" "$OPENSEARCH_ENDPOINT" "$S3_BUCKET_URL" <<'PYEOF'
import sys, re

html_file, os_url, s3_url = sys.argv[1], sys.argv[2], sys.argv[3]

with open(html_file) as f:
    content = f.read()

new_block = (
    '<script id="pulumi-config">\n'
    '    window.__PULUMI_CONFIG__ = {\n'
    f'      opensearchUrl: "{os_url}",\n'
    f'      s3BucketUrl:   "{s3_url}"\n'
    '    };\n'
    '  </script>'
)

# Match the entire <script id="pulumi-config">…</script> block
pattern = r'<script id="pulumi-config">.*?</script>'
updated, n = re.subn(pattern, new_block, content, flags=re.DOTALL)

if n == 0:
    print("✗  Could not find <script id=\"pulumi-config\"> in index.html", file=sys.stderr)
    sys.exit(1)

with open(html_file, "w") as f:
    f.write(updated)
PYEOF

echo "✓  index.html updated with live deployment values."
