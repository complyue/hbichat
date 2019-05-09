package main

import (
	"flag"
	"log"
	"net"
	"os"

	"github.com/complyue/hbi"
	service "github.com/complyue/hbichat/pkg/_service"
	"github.com/complyue/hbichat/pkg/errors"
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
	flag.StringVar(&servAddr, "serv", "localhost:3232", "HBI chat service address")
}

func main() {

	flag.Parse()

	defer func() {
		if err := recover(); err != nil {
			glog.Errorf("Unexpected error: %+v", errors.RichError(err))
			os.Exit(3)
		}
	}()

	hbi.ServeTCP(service.NewServiceEnv, servAddr, func(listener *net.TCPListener) {
		glog.Infof("HBI chat service listening: %s", listener.Addr())
	})

}
