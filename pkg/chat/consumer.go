// Consumer API for chat service.
package chat

import (
	"fmt"
	"io"
	"os"
	"time"

	hbi "github.com/complyue/hbigo"
	"github.com/complyue/hbigo/pkg/errors"
)

// interface for chat service consumers.
type ConsumerAPI struct {
	ctx *consumerContext

	Nick string
}

// get the hosting context to be used to establish a connection over HBI wire
func (api *ConsumerAPI) GetHoContext() hbi.HoContext {
	if api.ctx == nil {
		api.ctx = &consumerContext{
			HoContext: hbi.NewHoContext(),

			inRoom: "?.?",

			roomWelcome: make(chan string),
			msgPost:     make(chan string),
		}
	}
	return api.ctx
}

// an `io.Writer` to be passed in here, is an over simplified design.
// a GUI oriented API set can be defined, with much more, sophisticated methods,
// to facilitate a human facing display interface.
func (api *ConsumerAPI) SetOutput(output io.Writer) {
	api.ctx.output = func() io.Writer {
		return output
	}
}

func (api *ConsumerAPI) WaitWelcome() string {
	ctx := api.ctx
	msg := <-ctx.roomWelcome
	select {
	case s := <-ctx.msgPost:
		ctx.Show(s)
	case <-time.After(300 * time.Millisecond):
		// wait a bit before returning to prompt, in hope to see more notifications before prompt
	}
	return msg
}

func (api *ConsumerAPI) SetNick(nick string) {
	api.ctx.PoToPeer().Notif(fmt.Sprintf(`
SetNick(%#v)
`, nick))
	api.Nick = nick
}

func (api *ConsumerAPI) Goto(room string) {
	ctx := api.ctx
	ctx.goto_(room)
}

func (api *ConsumerAPI) Say(msg string) {
	ctx := api.ctx
	ctx.PoToPeer().Notif(fmt.Sprintf(`
Say(%#v)
`, msg))
	select {
	case s := <-ctx.msgPost:
		ctx.Show(s)
	case <-time.After(200 * time.Millisecond):
		// wait a bit before returning to prompt, in hope to see own msg above the prompt
	}
}

// one hosting context is created per consumer connection to a service.
// exported fields/methods are implementation details accommodating code from
// service, to materialize effects pushed by service.
// unexported fields/methods are implementation details necessary at
// consumer endpoint for house keeping etc.
type consumerContext struct {
	hbi.HoContext

	output func() io.Writer

	inRoom string

	roomWelcome chan string
	msgPost     chan string
}

// give types to be exposed, with nil pointer values to each
func (ctx *consumerContext) TypesToExpose() []interface{} {
	return []interface{}{
		(*MsgsInRoom)(nil),
		(*Msg)(nil),
	}
}

func (ctx *consumerContext) getOutput() io.Writer {
	if ctx.output != nil {
		return ctx.output()
	}
	return os.Stderr
}

func (ctx *consumerContext) goto_(room string) {
	ctx.PoToPeer().Notif(fmt.Sprintf(`
Goto(%#v)
`, room))
	select {
	case <-ctx.Done(): // already disconnected
		fmt.Fprintf(ctx.getOutput(), "Disconnected by server.")
	case msg := <-ctx.roomWelcome:
		fmt.Fprintln(ctx.getOutput(), msg)
	case <-time.After(2 * time.Second):
		fmt.Fprintf(ctx.getOutput(),
			"[%s] Not entered room #%s yet, maybe the service is busy.\n",
			time.Now().Format("15:04:05"), room)
	}
	select {
	case s := <-ctx.msgPost:
		ctx.Show(s)
	case <-time.After(300 * time.Millisecond):
		// wait a bit before returning to prompt, in hope to see recent msg log before prompt
	}
}

func (ctx *consumerContext) EnteredRoom(room string, welcomeMsg string) {
	ctx.inRoom = room
	select {
	case ctx.roomWelcome <- welcomeMsg:
	default:
		// the goto request has timed out waiting
		fmt.Fprintf(ctx.getOutput(), "[%s] Some late, but:\n%s\n",
			time.Now().Format("15:04:05"), welcomeMsg)
	}
}

func (ctx *consumerContext) RoomMsgs() {
	rms, err := ctx.Ho().CoRecvObj()
	if err != nil {
		panic(err)
	}
	var s string
	switch rms := rms.(type) {
	case *MsgsInRoom:
		s = rms.String()
	default:
		panic(errors.New(fmt.Sprintf("RoomMsgs got type %T ?!", rms)))
	}
	select {
	case <-ctx.Done(): // already disconnected
	case ctx.msgPost <- s: // try give it to whoever waiting
	case <-time.After(10 * time.Millisecond):
		// just print out if no one waiting atm
		fmt.Fprintln(ctx.getOutput(), s)
	}
}

func (ctx *consumerContext) Show(msg string) {
	fmt.Fprintln(ctx.getOutput(), msg)
}
