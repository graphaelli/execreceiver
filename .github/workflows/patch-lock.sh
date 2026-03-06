#!/usr/bin/env bash
# Patches pr-fix.lock.yml to support CLAUDE_CODE_OAUTH_TOKEN alongside ANTHROPIC_API_KEY.
# Run after `gh aw compile` to reapply OAuth token support.
set -euo pipefail

LOCK="${1:-.github/workflows/pr-fix.lock.yml}"

if [ ! -f "$LOCK" ]; then
  echo "error: $LOCK not found" >&2
  exit 1
fi

# 1. Validation step: accept either secret
sed -i.bak 's|validate_multi_secret\.sh ANTHROPIC_API_KEY |validate_multi_secret.sh ANTHROPIC_API_KEY,CLAUDE_CODE_OAUTH_TOKEN |' "$LOCK"

# 2. Add CLAUDE_CODE_OAUTH_TOKEN env var wherever ANTHROPIC_API_KEY is passed
sed -i.bak '/^          ANTHROPIC_API_KEY: \${{ secrets\.ANTHROPIC_API_KEY }}/a\
          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}' "$LOCK"

# 3. Secret redaction: add to secret names list and add SECRET_ variable
sed -i.bak "s|'ANTHROPIC_API_KEY,GH_AW_GITHUB_MCP_SERVER_TOKEN|'ANTHROPIC_API_KEY,CLAUDE_CODE_OAUTH_TOKEN,GH_AW_GITHUB_MCP_SERVER_TOKEN|" "$LOCK"
sed -i.bak '/^          SECRET_ANTHROPIC_API_KEY: \${{ secrets\.ANTHROPIC_API_KEY }}/a\
          SECRET_CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}' "$LOCK"

rm -f "${LOCK}.bak"

# Verify
count=$(grep -c 'CLAUDE_CODE_OAUTH_TOKEN' "$LOCK")
echo "patched $LOCK ($count references to CLAUDE_CODE_OAUTH_TOKEN)"
