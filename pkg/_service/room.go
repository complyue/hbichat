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
	sync.Mutex        // embed a mutex
	roomID            string
	firstMsg, lastMsg *MsgAtServer
	numMsgs           int
	chatters          map[*Chatter]struct{}
	cachedMsgLog      *ds.MsgsInRoom
}

func NewRoom(roomID string) *Room {
	return &Room{
		roomID:   roomID,
		chatters: make(map[*Chatter]struct{}),
	}
}

func (room *Room) Post(from, content string) {
	room.Lock()
	defer room.Unlock()

	now := time.Now()
	msg := &MsgAtServer{ds.Msg{from, content, now}, nil}
	if room.numMsgs <= 0 {
		if room.firstMsg != nil || room.lastMsg != nil {
			panic("?!")
		}
		room.firstMsg = msg
		room.lastMsg = msg
	} else {
		if room.lastMsg == nil {
			panic("?!")
		}
		for ; room.numMsgs >= ds.MaxHist; room.numMsgs-- {
			room.firstMsg = room.firstMsg.next
		}
		room.lastMsg.next = msg
		room.lastMsg = msg
	}
	room.numMsgs++
	// invalidate log cache
	room.cachedMsgLog = nil

	notifOut := &ds.MsgsInRoom{room.roomID, []ds.Msg{msg.Msg}}
	notifCode := fmt.Sprintf(`
RoomMsgs(%#v)`, notifOut)
	for chatter := range room.chatters {
		if chatter.inRoom != room {
			// went to another room, forget it
			delete(room.chatters, chatter)
		} else {
			// concurrently push new msg to it, forget it if err in notifying
			go func() {
				defer func() {
					if err := recover(); err != nil {
						room.Lock()
						defer room.Unlock()
						delete(room.chatters, chatter)
					}
				}()
				if err := chatter.po.Notif(notifCode); err != nil {
					panic(err)
				}
			}()
		}
	}
}

// all new comers see the cached log, until cache get invalidated (set to nil),
// when a new message is posted. but cache won't be generated until a new
// comer needs to see it
func (room *Room) recentMsgLog() *ds.MsgsInRoom {
	room.Lock()
	defer room.Unlock()

	if room.cachedMsgLog == nil {
		rms := &ds.MsgsInRoom{
			RoomID: room.roomID,
		}
		for msg := room.firstMsg; nil != msg; msg = msg.next {
			rms.Msgs = append(rms.Msgs, msg.Msg)
		}
		room.cachedMsgLog = rms
	}
	return room.cachedMsgLog
}

// MsgAtServer is a singly linked msg list for each room at server side,
// so as all tail pointers advance beyond a message record, it becomes eligible
// for garbage collection automatically
type MsgAtServer struct {
	ds.Msg
	next *MsgAtServer
}
