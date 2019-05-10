package consumer

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/complyue/hbi"
	"github.com/complyue/hbi/interop"
	"github.com/complyue/hbi/pkg/errors"
	"github.com/complyue/hbichat/pkg/ds"
	"github.com/complyue/liner"
	"github.com/golang/glog"
)

func NewConsumerEnv() *hbi.HostingEnv {
	he := hbi.NewHostingEnv()

	// expose names for interop
	interop.ExposeInterOpValues(he)

	// expose constructor functions for shared data structures
	he.ExposeCtor(ds.NewMsg, "")
	he.ExposeCtor(ds.NewMsgsInRoom, "")

	var (
		serviceAddr = "??"
		chatter     *Chatter
	)

	he.ExposeFunction("__hbi_init__", func(po hbi.PostingEnd, ho hbi.HostingEnd) {
		serviceAddr = fmt.Sprintf("%s", po.RemoteAddr())

		line := liner.NewLiner()
		chatter = &Chatter{
			line: line,
			po:   po, ho: ho,
			nick: "?", inRoom: "?",
			prompt: fmt.Sprintf(">%s> ", serviceAddr),
		}

		he.ExposeReactor(chatter)

		go func() {
			defer func() {

				line.Close()

				if e := recover(); e != nil {
					err := errors.RichError(e)
					ho.Disconnect(fmt.Sprintf("%+v", err), false)
				} else {
					ho.Close()
				}
			}()

			chatter.keepChatting()
		}()
	})

	he.ExposeFunction("__hbi_cleanup__", func(err error) {

		chatter.line.Close()

		if glog.V(1) {
			glog.Infof("Connection to chatting service %s lost: %+v", serviceAddr, err)
		}

	})

	return he
}

type Chatter struct {
	sync.Mutex // embed a mutex

	line *liner.State

	po hbi.PostingEnd
	ho hbi.HostingEnd

	nick     string
	inRoom   string
	sentMsgs []string

	prompt string
}

func (chatter *Chatter) setNick(nick string) {
	if err := chatter.po.Notif(fmt.Sprintf(`
SetNick(%#v)
`, nick)); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) gotoRoom(roomID string) {
	if err := chatter.po.Notif(fmt.Sprintf(`
GotoRoom(%#v)
`, roomID)); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) say(msg string) {

	// record msg to send in local log
	msgID := -1
	// try find an empty slot to hold this pending message
	for i := range chatter.sentMsgs {
		if chatter.sentMsgs[i] == "" {
			msgID = i
			break
		}
	}
	if msgID < 0 { // extend a new slot for this pending message
		msgID = len(chatter.sentMsgs)
		chatter.Lock()
		chatter.sentMsgs = append(chatter.sentMsgs, msg)
		chatter.Unlock()
	} else {
		chatter.Lock()
		chatter.sentMsgs[msgID] = msg
		chatter.Unlock()
	}

	// prepare binary data
	msgBuf := []byte(msg)
	// showcase notif with binary payload
	if err := chatter.po.NotifData(fmt.Sprintf(`
Say(%d, %d)
`, msgID, len(msgBuf)), msgBuf); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) keepChatting() {

	defer func() {
		if e := recover(); e != nil {
			err := errors.RichError(e)
			glog.Errorf("Unexpected error: %+v", err)
		}

		fmt.Println("\nBye.")
	}()

	for {
		select {
		case <-chatter.po.Done(): // disconnected from chat service
			return
		default: // still connected
		}

		code, err := chatter.line.Prompt(chatter.prompt)
		if err != nil {
			switch err {
			case io.EOF: // Ctrl^D to end chatting
			case liner.ErrPromptAborted: // Ctrl^C to giveup whatever input
				continue
			default:
				panic(err)
			}
			break
		}
		if len(strings.TrimSpace(code)) < 1 {
			// only white space(s) or just enter pressed
			continue
		}
		chatter.line.AppendHistory(code)

		if code[0] == '#' {
			// goto the specified room
			roomID := strings.TrimSpace(code[1:])
			chatter.gotoRoom(roomID)
		} else if code[0] == '$' {
			// change nick
			nick := strings.TrimSpace(code[1:])
			chatter.setNick(nick)
		} else {
			msg := code
			chatter.say(msg)
		}
	}

}

func (chatter *Chatter) updatePrompt() {
	chatter.prompt = fmt.Sprintf("%s@%s#%s: ", chatter.nick, chatter.po.RemoteAddr(), chatter.inRoom)
	chatter.line.ChangePrompt(chatter.prompt)
}

func (chatter *Chatter) NickAccepted(nick string) {
	chatter.Lock()
	defer chatter.Unlock()

	chatter.nick = nick
	chatter.updatePrompt()
}

func (chatter *Chatter) InRoom(roomID string) {
	chatter.Lock()
	defer chatter.Unlock()

	chatter.inRoom = roomID
	chatter.updatePrompt()
}

func (chatter *Chatter) RoomMsgs(roomMsgs *ds.MsgsInRoom) {
	chatter.line.HidePrompt()
	if roomMsgs.RoomID != chatter.inRoom {
		fmt.Printf(" *** Messages from #%s ***\n", roomMsgs.RoomID)
	}
	for i := range roomMsgs.Msgs {
		fmt.Printf("%+v\n", &roomMsgs.Msgs[i])
	}
	chatter.line.ShowPrompt()
}

func (chatter *Chatter) Said(msgID int) {
	chatter.Lock()
	msg := chatter.sentMsgs[msgID]
	chatter.sentMsgs[msgID] = ""
	chatter.Unlock()

	chatter.line.HidePrompt()
	fmt.Printf("@@ Your message [%d] has been displayed:\n  > %s\n", msgID, msg)
	chatter.line.ShowPrompt()
}

func (chatter *Chatter) ShowNotice(text string) {
	chatter.line.HidePrompt()
	fmt.Println(text)
	chatter.line.ShowPrompt()
}

func (chatter *Chatter) ChatterJoined(nick string, roomID string) {
	chatter.line.HidePrompt()
	fmt.Printf("@@ %s has joined #%s\n", nick, roomID)
	chatter.line.ShowPrompt()
}

func (chatter *Chatter) ChatterLeft(nick string, roomID string) {
	chatter.line.HidePrompt()
	fmt.Printf("@@ %s has left #%s\n", nick, roomID)
	chatter.line.ShowPrompt()
}
