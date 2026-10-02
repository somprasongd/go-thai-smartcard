module github.com/somprasongd/go-thai-smartcard

go 1.27.0

require (
	fyne.io/systray v1.12.2
	github.com/BurntSushi/toml v1.3.2
	github.com/ebfe/scard v0.0.0-20190212122703-c3d1b1916a95
	github.com/godbus/dbus/v5 v5.1.0
	github.com/googollee/go-socket.io v1.6.2
	github.com/gorilla/websocket v1.5.0
	github.com/kardianos/service v1.2.2
	github.com/varokas/tis620 v0.0.0-20150423070520-3d162af2a2ad
	golang.org/x/sys v0.48.0
)

require (
	github.com/gofrs/uuid v4.0.0+incompatible // indirect
	github.com/gomodule/redigo v1.8.4 // indirect
)

// Local Engine.IO shutdown fix; see third_party/go-socket.io/PATCHES.md.
replace github.com/googollee/go-socket.io => ./third_party/go-socket.io
