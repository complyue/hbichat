package service

import (
	"fmt"
	"strings"
	"sync"

	"github.com/complyue/hbi"
	"github.com/complyue/hbi/interop"
	"github.com/complyue/hbichat/pkg/ds"
	"github.com/golang/glog"
)

func NewServiceEnv() *hbi.HostingEnv {
	he := hbi.NewHostingEnv()

	// expose names for interop
	interop.ExposeInterOpValues(he)

	// expose constructor functions for shared types
	he.ExposeCtor(ds.NewMsg, "")
	he.ExposeCtor(ds.NewMsgsInRoom, "")

	var (
		consumerAddr = "??"
		chatter      *Chatter
	)

	he.ExposeFunction("__hbi_init__", func(po *hbi.PostingEnd, ho *hbi.HostingEnd) {
		consumerAddr = fmt.Sprintf("%s", po.RemoteAddr())

		chatter = &Chatter{
			po: po, ho: ho,

			inRoom: prepareRoom(""),
			nick:   fmt.Sprintf("Stranger$%s", consumerAddr),
		}

		he.ExposeReactor(chatter)

		chatter.welcomeChatter()
	})

	he.ExposeFunction("__hbi_cleanup__", func(discReason string) {
		if len(discReason) > 0 {
			glog.Infof("Connection to chatting consumer %s lost: %s", consumerAddr, discReason)
		} else if glog.V(1) {
			glog.Infof("Chatting consumer %s disconnected.", consumerAddr)
		}

		// remove chatter from its last room
		chatter.inRoom.Lock()
		delete(chatter.inRoom.chatters, chatter)
		chatter.inRoom.Unlock()
	})

	return he
}

type Chatter struct {
	po *hbi.PostingEnd
	ho *hbi.HostingEnd

	inRoom *Room
	nick   string

	mu sync.Mutex
}

func (chatter *Chatter) welcomeChatter() {

	func() { // send welcome notice to new comer
		co, err := chatter.po.NewCo()
		if err != nil {
			panic(err)
		}
		defer co.Close()

		var welcomeText strings.Builder
		welcomeText.WriteString(fmt.Sprintf(`
@@ Welcome %s, this is chat service at %s !
 -
@@ There're %d room(s) open, and you are in #%s now.
`, chatter.nick, chatter.ho.LocalAddr(), len(rooms), chatter.inRoom.roomID))
		for roomID, room := range rooms {
			welcomeText.WriteString(fmt.Sprintf("  -*-\t%d chatter(s) in room #%s\n", len(room.chatters), roomID))
		}
		if err = co.SendCode(fmt.Sprintf(`
NickChanged(%#v)
InRoom(%#v)
ShowNotice(%#v)
`, chatter.nick, chatter.inRoom.roomID, welcomeText.String())); err != nil {
			panic(err)
		}
	}()

	// send new comer info to other chatters already in room
	func() {
		// add this chatter into its 1st room
		chatter.inRoom.Lock()
		chatter.inRoom.chatters[chatter] = struct{}{}
		chatter.inRoom.Unlock()

		for otherChatter := range chatter.inRoom.chatters {
			if otherChatter == chatter {
				continue // don't notify self
			}
			if err := otherChatter.po.Notif(fmt.Sprintf(`
ChatterJoined(%#v, %#v)
`, chatter.nick, chatter.inRoom.roomID)); err != nil {
				glog.Errorf("Failed delivering room entering msg to %s", otherChatter.po.RemoteAddr())
			}
		}
	}()
}

func (chatter *Chatter) SetNick(nick string) {
	// note: the nick can be moderated here
	nick = strings.TrimSpace(nick)
	if nick == "" {
		nick = fmt.Sprintf("Stranger$%s", chatter.po.RemoteAddr())
	}
	chatter.mu.Lock()
	chatter.nick = nick
	chatter.mu.Unlock()

	// peer expects the moderated new nick be sent back within the conversation
	if err := chatter.ho.Co().SendObj(fmt.Sprintf("%#v", chatter.nick)); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) GotoRoom(roomID string) {
	oldRoom := chatter.inRoom
	newRoom := prepareRoom(roomID)

	func() { // leave old room
		oldRoom.Lock()
		delete(oldRoom.chatters, chatter)
		oldRoom.Unlock()

		for otherChatter := range oldRoom.chatters {
			if err := otherChatter.po.Notif(fmt.Sprintf(`
ChatterLeft(%#v, %#v)
`, chatter.nick, oldRoom.roomID)); err != nil {
				glog.Errorf("Failed delivering room leaving msg to %s", otherChatter.po.RemoteAddr())
			}
		}
	}()

	func() { // enter new room
		for otherChatter := range newRoom.chatters {
			if err := otherChatter.po.Notif(fmt.Sprintf(`
ChatterJoined(%#v, %#v)
`, chatter.nick, newRoom.roomID)); err != nil {
				glog.Errorf("Failed delivering room entering msg to %s", otherChatter.po.RemoteAddr())
			}
		}

		newRoom.Lock()
		newRoom.chatters[chatter] = struct{}{}
		newRoom.Unlock()
	}()

	// change record state
	chatter.mu.Lock()
	chatter.inRoom = newRoom
	chatter.mu.Unlock()

	// send feedback
	var welcomeText strings.Builder
	welcomeText.WriteString(fmt.Sprintf(`
@@ You are in #%s now, %d chatter(s).
`, newRoom.roomID, len(newRoom.chatters)))
	roomMsgs := newRoom.recentMsgLog()
	if err := chatter.ho.Co().SendCode(fmt.Sprintf(`
InRoom(%#v)
ShowNotice(%#v)
RoomMsgs(%#v)
`, newRoom.roomID, welcomeText.String(), roomMsgs)); err != nil {
		panic(err)
	}

}

// Say showcase a service method with binary payload, that to be received from
// current hosting conversation
func (chatter *Chatter) Say(msgID int, msgLen int) {

	// decode input data
	msgBuf := make([]byte, msgLen)
	if err := chatter.ho.Co().RecvData(msgBuf); err != nil {
		panic(err)
	}

	// use the input data
	msg := string(msgBuf)
	chatter.inRoom.Post(chatter, msg)

	// back-script the consumer to notify it about the success-of-display of the message
	if err := chatter.ho.Co().SendCode(fmt.Sprintf(`
Said(%d)
`, msgID)); err != nil {
		panic(err)
	}

}
