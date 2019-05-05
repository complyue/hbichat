/*
Common data structures for chat service.

*/
package chat

import (
	"fmt"
	"io"
	"time"
)

const (
	// each room keeps this many messages for history at max
	MaxHist = 10
)

type MsgsInRoom struct {
	Room string
	Msgs []Msg
}

type Msg struct {
	From    string
	Content string
	Time    time.Time
}

func (ml *MsgsInRoom) String() string {
	return fmt.Sprintf("%+v", ml)
}

func (ml *MsgsInRoom) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		io.WriteString(s, "#")
		io.WriteString(s, ml.Room)
		io.WriteString(s, "#:\n")
		for i, n := 0, len(ml.Msgs); i < n; i++ {
			(&ml.Msgs[i]).Format(s, verb)
			io.WriteString(s, "\n")
		}
	}
}

func (msg *Msg) String() string {
	return fmt.Sprintf("%+v", msg)
}

func (msg *Msg) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
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
