#!/usr/bin/env bash
# Setup script for apger credentials hostPath PVC
# Run this on the Kubernetes master node before applying k8s-manifest.yml

set -e

echo "=== APGer Credentials Setup ==="

# Get current username
USERNAME=$(whoami)
CREDS_DIR="$HOME/.apger/credentials"

echo "Username: $USERNAME"
echo "Credentials directory: $CREDS_DIR"

# Create credentials directory
if [ ! -d "$CREDS_DIR" ]; then
    echo "Creating credentials directory..."
    mkdir -p "$CREDS_DIR"
    chmod 700 "$HOME/.apger"
    chmod 700 "$CREDS_DIR"
    echo "✓ Created $CREDS_DIR"
else
    echo "✓ Credentials directory already exists"
fi

# Update k8s-manifest.yml with correct username
MANIFEST="k8s-manifest.yml"
if [ -f "$MANIFEST" ]; then
    echo "Updating $MANIFEST with username..."
    sed -i "s|/home/YOUR_USERNAME/.apger/credentials|$CREDS_DIR|g" "$MANIFEST"
    echo "✓ Updated hostPath in $MANIFEST"
else
    echo "⚠ Warning: $MANIFEST not found in current directory"
fi

echo ""
echo "=== Next Steps ==="
echo "1. Apply Kubernetes manifest:"
echo "   kubectl apply -f k8s-manifest.yml"
echo ""
echo "2. Verify PVC is bound:"
echo "   kubectl get pvc -n apger"
echo ""
echo "3. Attach to apger pod:"
echo "   kubectl attach -it apger -n apger"
echo ""
echo "4. In TUI, press 'c' to open credentials screen"
echo "5. Press 'n' to add a new maintainer"
echo "6. Fill in credentials and press ctrl+s to save"
echo ""
echo "Credentials will be stored at: $CREDS_DIR/apger.db"
