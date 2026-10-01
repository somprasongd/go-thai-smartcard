#!/bin/sh
# preinstall runs before the files are unpacked, so the service user exists
# when dpkg/rpm sets ownership on /etc/thai-smartcard.

set -e

if ! id thai-smartcard >/dev/null 2>&1; then
    useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin \
        --comment "Thai Smartcard Agent" thai-smartcard
fi
