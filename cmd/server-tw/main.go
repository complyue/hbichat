// Service reacting context testing-water

package main

import (
	"flag"
	"log"

	service "github.com/complyue/hbichat/pkg/_service"

	"github.com/complyue/hbi/pkg/repl"
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

func main() {
	flag.Parse()

	he := service.NewServiceEnv()

	repl.ReplWith(he)
}
