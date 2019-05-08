package main

import (
	"flag"
	"log"
	"net"

	"github.com/complyue/hbi"
	service "github.com/complyue/hbichat/pkg/_service"
	"github.com/golang/glog"
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

	hbi.ServeTCP(service.NewServiceContext, servAddr, func(listener *net.TCPListener) {
		log.Println("HBI chat service listening:", listener.Addr())
	})

}
