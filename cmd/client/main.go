package main

import (
	"flag"
	"fmt"
	"log"

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

		consumer.Cleanup()

		if err := recover(); err != nil {
			glog.Errorf("Unexpected error: %+v", errors.RichError(err))
		}

		fmt.Println("\nBye.")

	}()

	po, ho, err := hbi.DialTCP(servAddr, consumer.NewConsumerEnv())

	if err != nil {
		panic(err)
	}

	glog.V(1).Infof("Connected to chat service at: %s", po.RemoteAddr())

	select {
	case <-ho.Context().Done():
	}

	glog.V(1).Infof("Disconnected from chat service at: %s", po.RemoteAddr())

}
