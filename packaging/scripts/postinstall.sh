#!/bin/sh
# The package installs and starts the agent service; the tray installer depends
# on this one having run (decision 12: installers install and start it).

set -e

systemctl daemon-reload >/dev/null 2>&1 || true
systemctl enable --now thai-smartcard-agent >/dev/null 2>&1 || true
