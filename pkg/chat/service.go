// Business logic implementation for the chat service.
package chat

import (
	"fmt"
	"sync"
	"time"

	hbi "github.com/complyue/hbigo"
)

// create a service hosting context to accommodate a new connection from consumer
// over HBI wire.
func NewServiceContext() hbi.HoContext {
	return &ServiceContext{
		HoContext: hbi.NewHoContext(),
		Nick:      "stranger",
		inRoom:    lobby,
	}
}

// one hosting context will be created for each and every consumer connections.
// exported fields/methods are exposed service API,
// while unexported fields/methods are implementation details.
type ServiceContext struct {
	hbi.HoContext

	Nick   string
	inRoom *Room
}

// intercept p2p to initialize Nick name from network identity, and show this
// newly connected client to lobby
func (ctx *ServiceContext) SetPoToPeer(p2p hbi.Posting) {
	ctx.HoContext.SetPoToPeer(p2p)
	if p2p != nil {
		ctx.Nick = fmt.Sprintf("<Stranger@%s>", p2p.RemoteAddr())
		ctx.Goto("") // goto lobby
	}
}

func (ctx *ServiceContext) Goto(roomId string) {
	room := prepareRoom(roomId)
	ctx.inRoom = room
	room.stayers[ctx] = struct{}{}

	welcomeMsg := fmt.Sprintf("[%s] Welcome %s, %d in this room now.",
		room.name, ctx.Nick, len(room.stayers))
	p2p := ctx.PoToPeer()
	p2p.Notif(fmt.Sprintf(`
EnteredRoom(%#v,%#v)
`, roomId, welcomeMsg))
	msgLog := room.recentMsgLog()
	if len(msgLog.Msgs) > 0 {
		p2p.NotifBSON(`
RoomMsgs()
`, msgLog, "new(MsgsInRoom)")
	} else {
		p2p.Notif(`
Show("Too silent, you be the first speaker? ;-)")
`)
	}
}

func (ctx *ServiceContext) Say(msg string) {
	ctx.inRoom.Post(ctx.Nick, msg)
}

// use a singly linked msg list for each room at service side
// so as all tail pointers advance beyond a message record, it becomes eligible
// for garbage collection automatically
type MsgAtServer struct {
	Msg
	next *MsgAtServer
}

// room struct resides in service side only
type Room struct {
	sync.Mutex        // embed a mutex
	name              string
	firstMsg, lastMsg *MsgAtServer
	numMsgs           int
	stayers           map[*ServiceContext]struct{}
	cachedMsgLog      *MsgsInRoom
}

func NewRoom(id string) *Room {
	return &Room{
		name:    "(@" + id + "@)",
		stayers: make(map[*ServiceContext]struct{}),
	}
}

func (room *Room) Post(from, content string) {
	room.Lock()
	defer room.Unlock()

	now := time.Now()
	msg := &MsgAtServer{Msg{from, content, now}, nil}
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
		for ; room.numMsgs >= MaxHist; room.numMsgs-- {
			room.firstMsg = room.firstMsg.next
		}
		room.lastMsg.next = msg
		room.lastMsg = msg
	}
	room.numMsgs++
	// invalidate log cache
	room.cachedMsgLog = nil

	notifOut := &MsgsInRoom{room.name, []Msg{msg.Msg}}
	for stayer := range room.stayers {
		if stayer.inRoom != room {
			// went to another room, forget it
			delete(room.stayers, stayer)
		} else if p2p := stayer.PoToPeer(); p2p != nil {
			// concurrently push new msg to it, forget it if err in notifying
			go func() {
				defer func() {
					if err := recover(); err != nil {
						room.Lock()
						defer room.Unlock()
						delete(room.stayers, stayer)
					}
				}()
				p2p.NotifBSON(`
RoomMsgs()
`, notifOut, "new(MsgsInRoom)")
			}()
		} else {
			// disconnected or so, forget it
			delete(room.stayers, stayer)
		}
	}
}

// all new comers see the cached log, until cache get invalidated (set to nil),
// when a new message is posted. but cache won't be generated until a new
// comer needs to see it
func (room *Room) recentMsgLog() *MsgsInRoom {
	if room.cachedMsgLog == nil {
		rms := &MsgsInRoom{
			Room: room.name,
		}
		for msg := room.firstMsg; nil != msg; msg = msg.next {
			rms.Msgs = append(rms.Msgs, msg.Msg)
		}
		room.cachedMsgLog = rms
	}
	return room.cachedMsgLog
}

// global states for a service instance
var (
	mu    sync.Mutex
	lobby = NewRoom("Lobby")
	rooms = make(map[string]*Room)
)

func init() {
	rooms["Lobby"] = lobby
}

func prepareRoom(roomId string) (room *Room) {
	var ok bool
	room = lobby
	if "" != roomId {
		mu.Lock()
		defer mu.Unlock()
		room, ok = rooms[roomId]
		if !ok {
			room = NewRoom(roomId)
			rooms[roomId] = room
		}
	}
	return
}
