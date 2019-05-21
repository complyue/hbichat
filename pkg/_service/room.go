package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/complyue/hbichat/pkg/ds"
	"github.com/golang/glog"
)

// global states for a chat service instance
var (
	rooms      = make(map[string]*Room)
	roomsMutex sync.Mutex
)

func prepareRoom(roomID string) (room *Room) {
	if roomID == "" {
		roomID = "Lobby"
	}
	roomsMutex.Lock()
	defer roomsMutex.Unlock()

	var ok bool
	if room, ok = rooms[roomID]; !ok {
		room = NewRoom(roomID)
		rooms[roomID] = room
	}
	return
}

// Room struct resides in server side only
type Room struct {
	sync.Mutex   // embed a mutex
	roomID       string
	msgs         []ds.Msg
	chatters     map[*Chatter]struct{}
	cachedMsgLog *ds.MsgsInRoom
}

func NewRoom(roomID string) *Room {
	return &Room{
		roomID:   roomID,
		chatters: make(map[*Chatter]struct{}),
	}
}

// snapshot current chatter list
func (room *Room) chatterList() []*Chatter {
	room.Lock()
	defer room.Unlock()

	chatters := make([]*Chatter, 0, len(room.chatters))
	for chatter := range room.chatters {
		chatters = append(chatters, chatter)
	}
	return chatters
}

func (room *Room) recentMsgLog() *ds.MsgsInRoom {
	room.Lock()
	defer room.Unlock()

	if room.cachedMsgLog == nil {
		room.cachedMsgLog = &ds.MsgsInRoom{
			room.roomID, room.msgs,
		}
	}
	return room.cachedMsgLog
}

func (room *Room) Post(from *Chatter, content string) {
	var msg *ds.Msg
	chatters := room.chatterList()

	func() {
		room.Lock()
		defer room.Unlock()

		now := time.Now()
		room.msgs = append(room.msgs, ds.Msg{from.nick, content, now})
		if len(room.msgs) > ds.MaxHist {
			room.msgs = room.msgs[len(room.msgs)-ds.MaxHist:]
		}
		msg = &room.msgs[len(room.msgs)-1]
		// invalidate log cache
		room.cachedMsgLog = nil
	}()

	go func() { // send notification to others in a separated goroutine to avoid deadlock,
		// which is possible when 2 ho co happens need to create po co to eachother.

		notifOut := &ds.MsgsInRoom{room.roomID, []ds.Msg{*msg}}
		notifCode := fmt.Sprintf(`
RoomMsgs(%#v)
`, notifOut)
		for _, chatter := range chatters {
			if chatter == from {
				continue // don't notif the OP
			}
			if err := chatter.po.Notif(notifCode); err != nil {
				glog.Errorf("Failed delivering msg to consumer %s - %+v", chatter.po.RemoteAddr(), err)
			}
		}
	}()
}
