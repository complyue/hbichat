package service

import (
	"fmt"

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

	he.ExposeFunction("__hbi_init__", func(po hbi.PostingEnd, ho hbi.HostingEnd) {
		consumerAddr = fmt.Sprintf("%s", po.RemoteAddr())

		chatter = &Chatter{
			po: po, ho: ho,

			inRoom: prepareRoom(""),
			nick:   fmt.Sprintf("Stranger$%s", consumerAddr),
		}

		he.ExposeReactor(chatter)

		chatter.welcomeChatter()
	})

	he.ExposeFunction("__hbi_cleanup__", func(err error) {
		if err != nil {
			glog.Infof("Connection to chatting consumer %s lost: %+v", consumerAddr, err)
		} else if glog.V(1) {
			glog.Infof("Chatting consumer %s disconnected.", consumerAddr)
		}

		// remove chatter from its last room
		delete(chatter.inRoom.chatters, chatter)
	})

	return he
}

type Chatter struct {
	po hbi.PostingEnd
	ho hbi.HostingEnd

	inRoom *Room
	nick   string
}

func (chatter *Chatter) welcomeChatter() {

	chatter.inRoom.chatters[chatter] = struct{}{}

}

func (chatter *Chatter) GotoRoom(roomID string) {
	oldRoom := chatter.inRoom
	newRoom := prepareRoom(roomID)

	delete(oldRoom.chatters, chatter)
	chatter.inRoom = newRoom
	newRoom.chatters[chatter] = struct{}{}

}

func (chatter *Chatter) Say(msg string) {
	chatter.inRoom.Post(chatter.nick, msg)
}
