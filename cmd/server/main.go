package main

import (
	"flag"
	"github.com/complyue/gochat/pkg/chat"
	"github.com/complyue/hbigo"
	"github.com/golang/glog"
	"log"
	"net"
)

func init() {
	// change glog default destination to stderr
	if glog.V(0) { // should always be true, mention glog so it defines its flags before we change them
		if err := flag.CommandLine.Set("logtostderr", "true"); nil != err {
			log.Printf("Failed changing glog default desitination, err: %s", err)
		}
	}
}

var (
	servAddr string
)

func init() {
	flag.StringVar(&servAddr, "serv", "localhost:3232", "HBI serving address")
}

func main() {

	flag.Parse()

	hbi.ServeTCP(chat.NewServiceContext, servAddr, func(listener *net.TCPListener) {
		log.Println("HBI chat serving:", listener.Addr())
	})

}
