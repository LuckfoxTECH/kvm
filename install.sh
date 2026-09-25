#!/bin/sh

set -e

if [ "$1" = "--uninstall" ]; then
    echo "Uninstalling TESmart KVM Switch integration..."
    if [ -f /userdata/picokvm/bin/kvm_app.oem.bak ]; then
        cp /userdata/picokvm/bin/kvm_app.oem.bak /userdata/picokvm/bin/kvm_app
    fi
    if [ -f /usr/bin/kvm_app.oem.bak ]; then
        cp /usr/bin/kvm_app.oem.bak /usr/bin/kvm_app
    fi
    echo "Restarting service..."
    killall kvm_app || true
    echo "Uninstallation complete."
    exit 0
fi

echo "Installing TESmart KVM Switch integration..."

# Check compatibility
if ! grep -q "rv1106" /proc/cpuinfo && ! uname -a | grep -q "buildroot"; then
    echo "Error: This device does not appear to be a compatible Luckfox PicoKVM."
    exit 1
fi

# Download enhanced binary
TMP_BIN="/tmp/kvm_app_tesmart"
echo "Downloading binary..."
curl -sSL -o "$TMP_BIN" "https://github.com/Simon-CR/kvm/releases/download/v0.1.4-tesmart/kvm_app"

chmod +x "$TMP_BIN"

# Backup and install
if [ -f /userdata/picokvm/bin/kvm_app ] && [ ! -f /userdata/picokvm/bin/kvm_app.oem.bak ]; then
    cp /userdata/picokvm/bin/kvm_app /userdata/picokvm/bin/kvm_app.oem.bak
fi
if [ -f /usr/bin/kvm_app ] && [ ! -f /usr/bin/kvm_app.oem.bak ]; then
    cp /usr/bin/kvm_app /usr/bin/kvm_app.oem.bak
fi

echo "Installing binary..."
cp "$TMP_BIN" /userdata/picokvm/bin/kvm_app
cp "$TMP_BIN" /usr/bin/kvm_app

echo "Restarting service..."
killall kvm_app || true
rm "$TMP_BIN"

echo "Installation complete!"
