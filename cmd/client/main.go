package main

import (
	"flag"
	"log"
	"os"

	"github.com/complyue/hbi"
	"github.com/complyue/hbichat/pkg/consumer"
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

	po, ho, err := hbi.DialTCP(consumer.NewConsumerEnv(), servAddr)

	if err != nil {
		panic(err)
	}

	glog.V(1).Infof("Connected to chat service at: %s", po.RemoteAddr())

	select {
	case <-po.Done():
	case <-ho.Done():
	}

}
