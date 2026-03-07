#!/usr/bin/env bash
set -euo pipefail

# Entrypoint for curl-pipe installation:
#   curl -fsSL https://raw.githubusercontent.com/pgEdge/control-plane/main/examples/walkthrough/install.sh | bash

# --- Codespaces check ---

if [ -n "${CODESPACES:-}" ]; then
  echo ""
  echo "  Running in Codespaces — everything is already set up."
  echo "  Continue with the walkthrough or run: bash examples/walkthrough/guide.sh"
  echo ""
  exit 0
fi

WORK_DIR="${WALKTHROUGH_DIR:-pgedge-cp-walkthrough}"
BRANCH="${WALKTHROUGH_BRANCH:-main}"
BASE_URL="https://raw.githubusercontent.com/pgEdge/control-plane/${BRANCH}"

# --- Header ---

echo ""
echo "  pgEdge Control Plane Walkthrough"
echo "  ================================"
echo ""

# --- Download walkthrough files (mirrors repo layout) ---

echo "  Downloading walkthrough files..."
mkdir -p "$WORK_DIR/examples/walkthrough"
mkdir -p "$WORK_DIR/docs"

FILES=(
  examples/walkthrough/guide.sh
  examples/walkthrough/runner.sh
  examples/walkthrough/setup.sh
  docs/walkthrough.md
)

for file in "${FILES[@]}"; do
  if ! curl -fsSL "$BASE_URL/$file" -o "$WORK_DIR/$file"; then
    echo "  Error: failed to download $file" >&2
    exit 1
  fi
done

chmod +x "$WORK_DIR/examples/walkthrough/guide.sh" \
         "$WORK_DIR/examples/walkthrough/setup.sh"

cd "$WORK_DIR"

# --- Run setup (prerequisites only) ---

bash examples/walkthrough/setup.sh

# --- Choose how to continue ---

echo ""
echo "  Setup complete! How would you like to continue?"
echo ""
echo "    1) Interactive Guide — step-by-step in this terminal"
echo "    2) Exit — I'll open the walkthrough in my editor"
echo ""
read -rp "  Choose [1/2]: " choice </dev/tty

case "$choice" in
  2)
    echo ""
    echo "  Open this file in your editor and run the commands in this terminal:"
    echo "    $(pwd)/docs/walkthrough.md"
    echo ""
    ;;
  *)
    echo ""
    bash examples/walkthrough/guide.sh
    ;;
esac
