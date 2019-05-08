package ds

import (
	"fmt"
	"io"
	"time"
)

const (
	MaxHist = 10
)

type MsgsInRoom struct {
	RoomID string
	Msgs   []Msg
}

func NewMsgsInRoom(roomID string, msgs_a []interface{}) *MsgsInRoom {
	msgs := make([]Msg, len(msgs_a))
	for i, m := range msgs_a {
		if msg, ok := m.(*Msg); ok {
			msgs[i] = *msg
		}
	}
	return &MsgsInRoom{
		RoomID: roomID,
		Msgs:   msgs,
	}
}

func (ml *MsgsInRoom) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('#') { // repr form
			io.WriteString(s, "MsgsInRoom(")
			io.WriteString(s, fmt.Sprintf("%#v", ml.RoomID))
			io.WriteString(s, ", [\n")
			for i := range ml.Msgs {
				io.WriteString(s, fmt.Sprintf("  %#v", &ml.Msgs[i]))
				io.WriteString(s, ",\n")
			}
			io.WriteString(s, "])")
		} else { // str form
			io.WriteString(s, "#")
			io.WriteString(s, ml.RoomID)
			io.WriteString(s, ":\n")
			for i, n := 0, len(ml.Msgs); i < n; i++ {
				(&ml.Msgs[i]).Format(s, verb)
				io.WriteString(s, "\n")
			}
		}
	}
}

func (ml *MsgsInRoom) String() string {
	return fmt.Sprintf("%+v", ml)
}

type Msg struct {
	From    string
	Content string
	Time    time.Time
}

func NewMsg(from, content string, time_a int64) *Msg {
	return &Msg{
		From: from, Content: content,
		Time: time.Unix(time_a, 0),
	}
}

func (msg *Msg) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('#') { // repr form
			io.WriteString(s, "Msg(")
			io.WriteString(s, fmt.Sprintf("%#v", msg.From))
			io.WriteString(s, ",")
			io.WriteString(s, fmt.Sprintf("%#v", msg.Content))
			io.WriteString(s, ",")
			io.WriteString(s, fmt.Sprintf("%d", msg.Time.Unix()))
			io.WriteString(s, ")")
		} else { // str form
			if s.Flag('+') {
				io.WriteString(s, "[")
				io.WriteString(s, msg.Time.Format("Jan 02 15:04:05Z07"))
				io.WriteString(s, "] ")
			}
			io.WriteString(s, msg.From)
			io.WriteString(s, ": ")
			io.WriteString(s, msg.Content)
		}
	}
}

func (msg *Msg) String() string {
	return fmt.Sprintf("%+v", msg)
}
