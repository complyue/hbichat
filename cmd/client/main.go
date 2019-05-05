package main

import (
	"flag"
	"fmt"
	"github.com/complyue/gochat/pkg/chat"
	"github.com/complyue/hbigo"
	"github.com/complyue/hbigo/pkg/errors"
	"github.com/golang/glog"
	"github.com/peterh/liner"
	"io"
	"log"
	"net"
	"os"
	"os/user"
)

func init() {
	var err error

	// change glog default destination to stderr
	if glog.V(0) { // should always be true, mention glog so it defines its flags before we change them
		if err = flag.CommandLine.Set("logtostderr", "true"); nil != err {
			log.Printf("Failed changing glog default desitination, err: %s", err)
		}
	}

}

var (
	peerAddr string
)

func init() {
	flag.StringVar(&peerAddr, "peer", "localhost:3232", "HBI peer address")
}

func main() {

	osUser, err := user.Current()
	if err != nil {
		panic(err)
	}

	defer func() {
		if err := recover(); err != nil {
			glog.Errorf("Unexpected error: %+v", errors.RichError(err))
			os.Exit(3)
		}
	}()

	flag.Parse()

	api := &chat.ConsumerAPI{}

	hbic, err := hbi.DialTCP(api.GetHoContext(), peerAddr)
	if err != nil {
		panic(errors.Wrap(err, "Connection error"))
	}
	defer hbic.Close()

	welcomeMsg := api.WaitWelcome()
	fmt.Fprintf(os.Stderr, "Connected to %s\n%s\n", peerAddr, welcomeMsg)
	api.SetOutput(os.Stderr)
	api.SetNick(fmt.Sprintf("%s%%%d", osUser.Name, hbic.Conn.LocalAddr().(*net.TCPAddr).Port))

	line := liner.NewLiner()
	defer line.Close()

	line.SetCtrlCAborts(true)

	for {

		if hbic.Posting.Cancelled() {
			break
		}

		code, err := line.Prompt(fmt.Sprintf("chat: %s> ", api.Nick))
		if err != nil {
			switch err {
			case io.EOF: // Ctrl^D
			case liner.ErrPromptAborted: // Ctrl^C
			default:
				panic(errors.RichError(err))
			}
			break
		}
		if len(code) < 1 {
			continue
		}
		line.AppendHistory(code)

		if code[0] == '#' {
			api.Goto(code[1:])
		} else {
			api.Say(code)
		}

	}

	log.Printf("Bye.")

}
