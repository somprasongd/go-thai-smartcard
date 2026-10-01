# Verifying the Linux packages (decision 17)

The plan requires the `.deb`/`.rpm` to be verified on real Debian, Ubuntu LTS
and Fedora hosts before the phase 2 packages ship. The packages are attached
to a release by the packaging workflow; download `thai-smartcard-agent_<v>_amd64.deb`
and `thai-smartcard-agent-<v>.x86_64.rpm` from the release page and run this
checklist on each host, on a machine with the card reader attached.

## Install

```sh
# Debian / Ubuntu
sudo apt install ./thai-smartcard-agent_<v>_amd64.deb

# Fedora
sudo dnf install ./thai-smartcard-agent-<v>.x86_64.rpm
```

The postinstall enables and starts the service; verify it stayed up:

```sh
id thai-smartcard                          # the dedicated system user exists
systemctl status thai-smartcard-agent      # active (running)
journalctl -u thai-smartcard-agent -b      # the agent's own log
```

## The polkit path (decision 17)

The service runs as `thai-smartcard`, not root. pcsc-lite enables polkit by
default upstream and denies any process without an active local session —
exactly what a system service is. The package installs the rule, so a healthy
host logs readers, not refusals:

```sh
cat /etc/polkit-1/rules.d/50-thai-smartcard.pcscd.rules
journalctl -u thai-smartcard-agent -b | grep -i reader
```

Expected in the log: `Available N readers:` with the reader listed.

**Failure signature:** `SCARD_W_SECURITY_VIOLATION` in the journal, with the
agent pointing at the rule. Then check, in order:

1. The rule file exists and names the service user (above).
2. `systemctl restart pcscd` and restart the agent — polkit decisions are
   evaluated per connection, but pcscd caches policies.
3. `busctl status` of pcscd — polkit must be compiled in (`polkit support: yes`
   in `pcscd --version` output). Where it is not, the rule is inert and the
   service should simply work; record which case the distro is.

Record the distro and version for each result: the plan names Debian, Ubuntu
LTS and Fedora as the hosts to cover.

## The agent itself

```sh
curl -s http://127.0.0.1:9898/api/info      # {version, transports, tls}
curl -s http://127.0.0.1:9898/api/settings  # config as served; token absent
```

Then, from a browser on the same host:

1. `http://localhost:9898` shows the test page; a card insert fills it in.
2. `/settings` saves: change `[card] read_nhso`, confirm the file is rewritten
   (`sudo cat /etc/thai-smartcard/config.toml`) and still owned by
   `thai-smartcard` mode `0600` — the agent is the only writer (decision 3),
   and it must stay able to write its own config as the service user.
3. Expose to network: save `listen = "0.0.0.0"` — the agent generates a token,
   restarts the listener, and `/ws` answers `401` without the token.
4. The tray package (`thai-smartcard-tray_<v>_amd64.deb`): installing it pulls
   the agent package (`Depends:`), `/etc/xdg/autostart/thai-smartcard-tray.desktop`
   exists, and the icon appears after re-login on GNOME **with** the
   AppIndicator extension; without it, a notification pointing at `/settings`
   instead.

## Package contents, for the record

Checked at build time (and worth re-checking on the host after install):

```sh
dpkg -L thai-smartcard-agent        # /usr/bin/thai-smartcard-agent,
                                    # /lib/systemd/system/thai-smartcard-agent.service,
                                    # /etc/polkit-1/rules.d/50-thai-smartcard.pcscd.rules,
                                    # /etc/thai-smartcard/config.toml
dpkg -e thai-smartcard-agent_<v>_amd64.deb  # preinst/postinst/postrm present
rpm -ql thai-smartcard-agent        # the same set on Fedora
```

`/etc/thai-smartcard` is `0700 thai-smartcard` and `config.toml` is `0600
thai-smartcard` in the package archive, so the service user owns its config
from the first boot.
