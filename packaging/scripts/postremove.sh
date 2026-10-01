#!/bin/sh
# Removing the package stops the service; the config file (which can hold the
# socket token) and the service user are left behind, so an upgrade across
# remove-and-reinstall keeps its settings.

set -e

systemctl disable --now thai-smartcard-agent >/dev/null 2>&1 || true
systemctl daemon-reload >/dev/null 2>&1 || true
