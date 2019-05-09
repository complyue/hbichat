// Service reacting context testing-water

package main

import (
	"flag"
	"log"

	"github.com/complyue/hbichat/pkg/consumer"

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

	he := consumer.NewConsumerEnv()

	repl.ReplWith(
		he, // hosting environment

		// debugged with delve as for now, no interactive input can be sent in,
		// put some banner script for debugging here as a workaround.

		// "dir()", // banner script
		"", // banner script
	)
}
