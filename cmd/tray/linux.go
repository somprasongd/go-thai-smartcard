//go:build linux

package main

import (
	"github.com/godbus/dbus/v5"
)

// checkStatusNotifier looks for the DBus name that GNOME's tray support lives
// behind. Without it (GNOME without the AppIndicator extension) the icon will
// never appear, so the tray sends a notification pointing at /settings
// instead of failing silently.
func checkStatusNotifier(agentURL string) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return
	}
	for _, n := range names {
		if n == "org.kde.StatusNotifierWatcher" {
			return
		}
	}
	notify(conn, "Thai Smartcard", "เปิด "+agentURL+"/settings เพื่อตั้งค่า — GNOME needs the AppIndicator extension to show a tray icon.")
}

// notify sends one desktop notification.
func notify(conn *dbus.Conn, summary, body string) {
	conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"Thai Smartcard", uint32(0), "", summary, body,
		[]string{}, map[string]dbus.Variant{}, int32(10000),
	)
}
