package main

import (
	"fmt"

	"github.com/complyue/hbi"
)

func main() {

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
	func() {
		defer co.Close()

		if err = co.SendCode(`
my_name = "Nick"
hello()	
`); err != nil {
			panic(err)
		}
	}()

	msgBack, err := co.RecvObj()
	if err != nil {
		panic(err)
	}
	fmt.Println(msgBack)
}
