#!/bin/bash
# setup.sh - Minimal AonMiner USB permission setup

if [ "$EUID" -ne 0 ]; then
    echo "ERROR: Run with sudo: sudo $0"
    exit 1
fi

# Create udev rules
cat > /etc/udev/rules.d/99-aonminer.rules << EOF
SUBSYSTEM=="usb", ATTR{idVendor}=="0403", ATTR{idProduct}=="6015", MODE="0666"
SUBSYSTEM=="tty", ATTRS{idVendor}=="0403", ATTRS{idProduct}=="6015", MODE="0666"
EOF

# Reload udev
if command -v udevadm >/dev/null 2>&1; then
    udevadm control --reload-rules
    udevadm trigger
    echo "Done. Unplug/replug devices."
else
    echo "ERROR: udevadm not found. Install udev or reboot."
fi
