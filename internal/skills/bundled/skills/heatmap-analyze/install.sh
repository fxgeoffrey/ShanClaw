#!/bin/sh
# Ptengine Heatmap — ptengine-cli installer
# Wraps the official install script with status checking.
#
# Usage:
#   sh install.sh              # Install (if needed) and show status
#   sh install.sh --check-only # Check status without installing
set -eu

# Pin upstream to a specific release tag to prevent unreviewed changes on
# main from affecting every ShanClaw user. Bump together with any
# ptengine-cli release validated against this skill.
PTENGINE_CLI_REF="v0.1.0"

CHECK_ONLY=false
if [ "${1-}" = "--check-only" ]; then
  CHECK_ONLY=true
fi

# ---------- helpers ----------
config_file="$HOME/.config/ptengine-cli/config.yaml"

has_cli() {
  command -v ptengine-cli >/dev/null 2>&1
}

has_config() {
  [ -f "$config_file" ] && grep -q "api_key:" "$config_file" 2>/dev/null
}

# ---------- check ----------
if has_cli; then
  VERSION=$(ptengine-cli version 2>/dev/null || echo "unknown")
  echo "ptengine-cli is installed.  ($VERSION)"

  if has_config; then
    echo "Configuration found at $config_file"
    ptengine-cli config show 2>/dev/null || true
    echo ""
    echo "STATUS: READY"
    exit 0
  else
    echo ""
    echo "WARNING: ptengine-cli is installed but not configured."
    echo "Run:  ptengine-cli config set --api-key <YOUR_API_KEY> --profile-id <YOUR_PROFILE_ID>"
    echo "Don't have an API Key? See references/ptengine-cli.md - 'Obtaining an API Key'."
    echo ""
    echo "STATUS: NEEDS_CONFIG"
    exit 1
  fi
fi

# ---------- not installed ----------
if [ "$CHECK_ONLY" = true ]; then
  echo "ptengine-cli is NOT installed."
  echo ""
  echo "STATUS: NOT_INSTALLED"
  exit 1
fi

# ---------- install ----------
echo "Installing ptengine-cli via official script..."
echo ""
curl -sSL "https://raw.githubusercontent.com/Kocoro-lab/ptengine-cli/${PTENGINE_CLI_REF}/scripts/install.sh" | sh

echo ""

if has_cli; then
  echo "Installation successful."
  echo ""
  echo "Next step — configure your API credentials:"
  echo "  ptengine-cli config set --api-key <YOUR_API_KEY> --profile-id <YOUR_PROFILE_ID>"
  echo "  (How to obtain a key: see references/ptengine-cli.md - 'Obtaining an API Key')"
  echo ""
  echo "STATUS: INSTALLED_NEEDS_CONFIG"
else
  echo "ERROR: Installation failed. ptengine-cli not found in PATH."
  echo ""
  echo "STATUS: INSTALL_FAILED"
  exit 1
fi
