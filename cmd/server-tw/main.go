// test the water with service reacting env

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

	bannerScript := `
ps1 = "HBICHAT Server:> "
print("\n### Exposed artifacts of current hosting env:\n")
dir()
`
	// if debugged with delve, no interactive input can be sent in and as a workaround,
	// add calls to suspicious stuff from above script to invoke the buggy cases.

	repl.ReplWith(he, bannerScript)
}
