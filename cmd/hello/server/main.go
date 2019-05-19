package main

import (
	"fmt"
	"net"

	"github.com/complyue/hbi"
)

func main() {

	hbi.ServeTCP("localhost:3232", func() *hbi.HostingEnv {
		he := hbi.NewHostingEnv()

		he.ExposeFunction("__hbi_init__", // callback on wire connected
			func(po *hbi.PostingEnd, ho *hbi.HostingEnd) {
				po.Notif(`
print("Hello, HBI world!")
`)
			})

		he.ExposeFunction("hello", func() {
			if err := he.Ho().Co().SendObj(hbi.Repr(fmt.Sprintf(
				`Hello, %s from %s!`,
				he.Get("my_name"), he.Po().RemoteAddr(),
			))); err != nil {
				panic(err)
			}
		})
		return he
	}, func(listener *net.TCPListener) {
		fmt.Println("hello server listening:", listener.Addr())
	})

}
