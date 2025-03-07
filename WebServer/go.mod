module webserver

go 1.24.0

require (
	celebration/Client v0.0.0-00010101000000-000000000000
	github.com/gorilla/websocket v1.5.3
)

require (
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rpi-ws281x/rpi-ws281x-go v1.0.10 // indirect
)

replace celebration/client => ../Client

replace celebration/Client => ../Client
