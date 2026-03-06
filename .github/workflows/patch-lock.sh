#!/usr/bin/env bash
# Patches pr-fix.lock.yml to use CLAUDE_CODE_OAUTH_TOKEN instead of ANTHROPIC_API_KEY.
# Run after `gh aw compile` to reapply OAuth token support.
set -euo pipefail

LOCK="${1:-.github/workflows/pr-fix.lock.yml}"

if [ ! -f "$LOCK" ]; then
  echo "error: $LOCK not found" >&2
  exit 1
fi

# 1. Validation step: check CLAUDE_CODE_OAUTH_TOKEN instead of ANTHROPIC_API_KEY
sed -i.bak 's|validate_multi_secret\.sh ANTHROPIC_API_KEY |validate_multi_secret.sh CLAUDE_CODE_OAUTH_TOKEN |' "$LOCK"
sed -i.bak '/Validate ANTHROPIC_API_KEY/{
  s|ANTHROPIC_API_KEY|CLAUDE_CODE_OAUTH_TOKEN|
}' "$LOCK"
sed -i.bak 's|^          ANTHROPIC_API_KEY: \${{ secrets\.ANTHROPIC_API_KEY }}|          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}|' "$LOCK"

# 2. Replace ANTHROPIC_API_KEY env var with CLAUDE_CODE_OAUTH_TOKEN in agent and threat detection steps
sed -i.bak '/^          ANTHROPIC_API_KEY: \${{ secrets\.ANTHROPIC_API_KEY }}/c\
          CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}' "$LOCK"

# 3. Secret redaction: replace ANTHROPIC_API_KEY with CLAUDE_CODE_OAUTH_TOKEN
sed -i.bak "s|'ANTHROPIC_API_KEY,GH_AW_GITHUB_MCP_SERVER_TOKEN|'CLAUDE_CODE_OAUTH_TOKEN,GH_AW_GITHUB_MCP_SERVER_TOKEN|" "$LOCK"
sed -i.bak 's|SECRET_ANTHROPIC_API_KEY: \${{ secrets\.ANTHROPIC_API_KEY }}|SECRET_CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}|' "$LOCK"

rm -f "${LOCK}.bak"

# Verify
if grep -q 'secrets\.ANTHROPIC_API_KEY' "$LOCK"; then
  echo "warning: residual ANTHROPIC_API_KEY references remain:" >&2
  grep -n 'secrets\.ANTHROPIC_API_KEY' "$LOCK" >&2
  exit 1
fi
count=$(grep -c 'CLAUDE_CODE_OAUTH_TOKEN' "$LOCK")
echo "patched $LOCK ($count references to CLAUDE_CODE_OAUTH_TOKEN, 0 to ANTHROPIC_API_KEY)"
