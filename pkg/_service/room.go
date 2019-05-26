package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/complyue/hbichat/pkg/ds"
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

// enumerate chatters in room, drop those disconnected and causing errors
func (room *Room) eachInRoom(withChatter func(chatter *Chatter) error) {
	var errChatters []*Chatter
	for _, chatter := range room.chatterList() {
		if err := withChatter(chatter); err != nil {
			if chatter.po.Disconnected() {
				errChatters = append(errChatters, chatter)
			}
		}
	}
	if len(errChatters) <= 0 {
		return
	}

	room.Lock()
	defer room.Unlock()

	for _, chatter := range errChatters {
		delete(room.chatters, chatter)
	}
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

	// notify all chatters but the OP in this room about the new msg
	notifOut := &ds.MsgsInRoom{room.roomID, []ds.Msg{*msg}}
	notifCode := fmt.Sprintf(`
RoomMsgs(%#v)
`, notifOut)

	// send notification to others in a separated goroutine to avoid deadlock
	go room.eachInRoom(func(chatter *Chatter) error {
		if chatter == from {
			return nil // not to the OP
		}
		return chatter.po.Notif(notifCode)
	})
}
