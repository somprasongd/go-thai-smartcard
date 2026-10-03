[← README](../README.md)

## Reader requirements

A card reader has to be visible to PC/SC before anything will read a card. Check
what is visible:

```sh
go run ./cmd/record -list
```

If it prints no readers, the reader is not reachable and installing the
toolchain will not help.

| Platform | What is needed |
| :------- | :------------- |
| **Linux** | `sudo apt install build-essential libpcsclite-dev pcscd`. The CCID driver in `libccid` is normally already present. |
| **Windows** | The reader vendor's driver. For an Identiv uTrust, the `Identiv uTrust Installer` package picks the right driver for the Windows version. |
| **macOS** | Nothing to download. If the reader does not show up or connects fail, see below. |

Recording a trace does not have to happen on the same machine as the agent.
`cmd/record` is cross platform, so it can be built and run wherever the reader
works:

```sh
GOOS=windows go build -o record.exe ./cmd/record
```

### If macOS cannot see the reader

macOS carries **two** CCID drivers:

- `/usr/libexec/SmartCardServices/drivers/ifd-ccid.bundle` — IFD CCID, the open
  source `libccid` driver.
- `/System/Library/CryptoTokenKit/usbsmartcardreaderd.slotd` — Apple's own
  driver, which matches readers by USB interface *class* rather than by vendor
  and product ID.

Most readers work with either. When one does not, switching to IFD CCID usually
fixes it. Both are user space, so do not go looking in
`/System/Library/Extensions`:

```sh
sudo defaults write /Library/Preferences/com.apple.security.smartcard useIFDCCID -bool true
sudo killall usbsmartcardreaderd
```

Then unplug and replug the reader. A reboot is not needed:
`com.apple.usbsmartcardreaderd` is an on-demand launchd daemon whose launch event
fires when a USB interface with `bInterfaceClass = 11` appears, so replugging
starts it again and it picks up the new setting. Verify with
`go run ./cmd/record -list`.

The Identiv uTrust 2700 R is in IFD CCID's table as `0x04E6:0x5810`. If the
IFD CCID version macOS ships is too old for your reader, Thales publishes a
newer CCID installer at
[supportportal.thalesgroup.com KB0027738](https://supportportal.thalesgroup.com/csm?id=kb_article_view&sysparm_article=KB0027738).

### If a read fails with a sharing violation

`scard: Sharing violation` means another handle has the card. The agent
connects shared and locks the card with a transaction for the length of a
read, so it coexists with the handle macOS parks on every inserted PKI card,
and when a connect still loses the race it keeps retrying whole reads for as
long as the card stays inserted: one error naming the holder, then the data
the moment the way in opens.

When the error says the card is **locked exclusively**, retrying cannot win —
PC/SC has no way to take a card back from another handle. Remove the card and
insert it again. That is also the way out of a lock left behind by an app
force-quit while holding the card, which survives the app's death inside the
PC/SC broker. As a last resort the broker itself can be restarted; launchd
starts it again on demand:

```sh
sudo killall ctkpcscd
```

If the card keeps getting seized on insert, macOS is claiming it as an
identity token. Tell CryptoTokenKit to leave the card alone, then replug the
reader (a reboot is the certain way to apply it):

```sh
sudo defaults write /Library/Preferences/com.apple.security.smartcard DisabledTokens -array com.apple.CryptoTokenKit.pivtoken
```

`pivtoken` is the built-in driver macOS offers for third-party PKI cards and
the one Apple documents disabling. With the card seated,
`system_profiler SPSmartCardsDataType` shows which driver claimed it under
"SmartCard Drivers" and whether it is currently held as a token under
"Available SmartCards". Undo with
`sudo defaults delete /Library/Preferences/com.apple.security.smartcard DisabledTokens`.

On Linux, check for other PC/SC clients (a second agent, a browser doing
certificate lookups) with `pcscd --foreground --debug` in the background.
