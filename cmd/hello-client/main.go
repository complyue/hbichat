package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/complyue/hbi"
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

	he := hbi.NewHostingEnv()

	he.ExposeFunction("print", fmt.Println)

	po, ho, err := hbi.DialTCP("localhost:3232", he)
	if err != nil {
		panic(err)
	}
	defer ho.Close()

	co, err := po.NewCo()
	if err != nil {
		panic(err)
	}
	defer co.Close()

	if err = co.SendCode(`
my_name = "Nick"
hello()	
`); err != nil {
		panic(err)
	}

	co.StartRecv()

	msgBack, err := co.RecvObj()
	if err != nil {
		panic(err)
	}
	fmt.Println(msgBack)
}
