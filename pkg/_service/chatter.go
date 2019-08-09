package service

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
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

	he.ExposeFunction("__hbi_cleanup__", func(
		po *hbi.PostingEnd,
		ho *hbi.HostingEnd,
		discReason string,
	) {
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

// Chatter defines service side chatter object
type Chatter struct {
	po *hbi.PostingEnd
	ho *hbi.HostingEnd

	inRoom *Room
	nick   string

	mu sync.Mutex
}

// NamesToExpose declares names of chatter methods to be exposed to an HBI `HostingEnv`,
// when a chatter object is exposed as a reactor with `he.ExposeReactor(chatter)`.
//
// Note: this is optional, and if omitted, all exported (according to Golang rule, whose
// name starts with a capital letter) methods and fields from the `chatter` object,
// including those inherited from embedded fields, are exposed.
func (chatter *Chatter) NamesToExpose() []string {
	return []string{
		"SetNick",
		"GotoRoom",
		"Say",
		"UploadReq",
		"RecvFile",
		"ListFiles",
		"SendFile",
	}
}

func (chatter *Chatter) welcomeChatter() {
	nick := chatter.nick // snapshot the value to avoid dirty reads in following goroutines

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
`, nick, chatter.ho.LocalAddr(), len(rooms), chatter.inRoom.roomID))
		for roomID, room := range rooms {
			room.Lock()
			nchatters := len(room.chatters)
			room.Unlock()
			welcomeText.WriteString(fmt.Sprintf("  -*-\t%d chatter(s) in room #%s\n", nchatters, roomID))
		}
		if err = co.SendCode(fmt.Sprintf(`
NickChanged(%#v)
InRoom(%#v)
ShowNotice(%#v)
`, nick, chatter.inRoom.roomID, welcomeText.String())); err != nil {
			panic(err)
		}
	}()

	// add this chatter into its 1st room
	chatter.inRoom.Lock()
	chatter.inRoom.chatters[chatter] = struct{}{}
	chatter.inRoom.Unlock()

	// start new po co to others in new goroutines to avoid deadlocks
	go chatter.inRoom.eachInRoom(func(otherChatter *Chatter) error {
		if otherChatter == chatter {
			return nil // don't notif him/her self
		}
		if err := otherChatter.po.Notif(fmt.Sprintf(`
ChatterJoined(%#v, %#v)
`, nick, chatter.inRoom.roomID)); err != nil {
			glog.Errorf("Failed delivering room entering msg to %s", otherChatter.po.RemoteAddr())
			return err
		}
		return nil
	})
}

func (chatter *Chatter) SetNick(nick string) {
	co := chatter.ho.Co()
	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	// note: the nick can be moderated here
	nick = strings.TrimSpace(nick)
	if nick == "" {
		nick = fmt.Sprintf("Stranger$%s", chatter.po.RemoteAddr())
	}
	chatter.mu.Lock()
	chatter.nick = nick
	chatter.mu.Unlock()

	// peer expects the moderated new nick be sent back
	if err := co.SendObj(fmt.Sprintf("%#v", chatter.nick)); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) GotoRoom(roomID string) {
	co := chatter.ho.Co()
	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	oldRoom := chatter.inRoom
	newRoom := prepareRoom(roomID)

	// leave old room, enter new room. writes should be sync'ed properly
	oldRoom.Lock()
	delete(oldRoom.chatters, chatter)
	oldRoom.Unlock()
	newRoom.Lock()
	newRoom.chatters[chatter] = struct{}{}
	newRoom.Unlock()

	// change record state
	chatter.mu.Lock()
	chatter.inRoom = newRoom
	nick := chatter.nick
	chatter.mu.Unlock()

	// send feedback
	var welcomeText strings.Builder
	welcomeText.WriteString(fmt.Sprintf(`
@@ You are in #%s now, %d chatter(s).
`, newRoom.roomID, len(newRoom.chatters)))
	roomMsgs := newRoom.recentMsgLog()
	if err := co.SendCode(fmt.Sprintf(`
InRoom(%#v)
ShowNotice(%#v)
RoomMsgs(%#v)
`, newRoom.roomID, welcomeText.String(), roomMsgs)); err != nil {
		panic(err)
	}

	// start new po co to others in new goroutines to avoid deadlocks
	go oldRoom.eachInRoom(func(otherChatter *Chatter) error {
		if otherChatter == chatter {
			return nil // skip him/her self
		}
		if err := otherChatter.po.Notif(fmt.Sprintf(`
ChatterLeft(%#v, %#v)
`, nick, oldRoom.roomID)); err != nil {
			glog.Errorf("Failed delivering room leaving msg to %s", otherChatter.po.RemoteAddr())
			return err
		}
		return nil
	})
	go newRoom.eachInRoom(func(otherChatter *Chatter) error {
		if otherChatter == chatter {
			return nil // skip him/her self
		}
		if err := otherChatter.po.Notif(fmt.Sprintf(`
ChatterJoined(%#v, %#v)
`, nick, newRoom.roomID)); err != nil {
			glog.Errorf("Failed delivering room entering msg to %s", otherChatter.po.RemoteAddr())
			return err
		}
		return nil
	})
}

// Say showcase a service method with binary payload, that to be received with
// current hosting conversation
func (chatter *Chatter) Say(msgID int, msgLen int) {
	co := chatter.ho.Co()

	// receive & decode input data
	msgBuf := make([]byte, msgLen)
	if err := co.RecvData(msgBuf); err != nil {
		panic(err)
	}

	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	// post the msg to current room
	msg := string(msgBuf)
	chatter.inRoom.Post(chatter, msg)

	// back-script the consumer to notify it about the success-of-display of the message
	if err := co.SendCode(fmt.Sprintf(`
Said(%d)
`, msgID)); err != nil {
		panic(err)
	}

}

func (chatter *Chatter) UploadReq(roomID string, fn string, fsz int64) {
	co := chatter.ho.Co()
	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	if fsz > 200*1024*1024 { // 200 MB at most
		// send the reason as string, why it's refused
		if err := co.SendObj(hbi.Repr("file too large!")); err != nil {
			panic(err)
		}
		return
	}
	if fsz < 2*1024 { // 2 KB at least
		// send the reason as string, why it's refused
		if err := co.SendObj(hbi.Repr("file too small!")); err != nil {
			panic(err)
		}
		return
	}

	// nil as refuse_reason means the upload is accepted
	if err := co.SendObj("nil"); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) RecvFile(roomID string, fn string, fsz int64) {
	co := chatter.ho.Co()

	// TODO in a real world application, the same validation rules as in UploadReq()
	// should be checked again, or it's a security hole that a consumer can exploit.

	roomDir, err := filepath.Abs(fmt.Sprintf("chat-server-files/%s", roomID))
	if err != nil {
		panic(err)
	}
	if err = os.MkdirAll(roomDir, 0755); err != nil {
		panic(err)
	}

	fpth := filepath.Join(roomDir, fn)

	// unlink the file before creating a new one, so if someone has opened it for
	// download, that can finish normally.
	os.Remove(fpth)
	f, err := os.Create(fpth)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// these need to be accessed both inside and outside of data stream cb, define here
	totalKB := int64(math.Ceil(float64(fsz) / 1024))
	var chksum uint32

	// recv data with one 1-KB-chunk at max at a time.
	bytesRemain := fsz
	chunk := make([]byte, 1024) // reused 1 KB buffer
	// receive data stream from client
	if err = co.RecvStream(func() ([]byte, error) {
		if bytesRemain < fsz { // last chunk has been received, write to file
			n := len(chunk)
			for d := chunk; len(d) > 0; d = d[n:] {
				if n, err = f.Write(d); err != nil {
					return nil, err
				}
			}
			// update chksum
			chksum = crc32.Update(chksum, crc32.IEEETable, chunk)
		}

		if bytesRemain <= 0 { // full file data has been received
			return nil, nil
		}

		if bytesRemain < int64(len(chunk)) {
			chunk = chunk[:bytesRemain]
		}
		bytesRemain -= int64(len(chunk))
		return chunk, nil
	}); err != nil {
		panic(err)
	}

	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	// send back chksum for client to verify
	if err = co.SendObj(hbi.Repr(chksum)); err != nil {
		panic(err)
	}

	// close the hosting conversation a.s.a.p.
	if err = co.Close(); err != nil {
		panic(err)
	}

	// announce this new upload
	chatter.inRoom.Post(chatter, fmt.Sprintf(`
 @*@ I just uploaded a file %x %d KB [%s]
`, chksum, totalKB, fn))
}

func (chatter *Chatter) ListFiles(roomID string) {
	co := chatter.ho.Co()
	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	roomDir, err := filepath.Abs(fmt.Sprintf("chat-server-files/%s", roomID))
	if err != nil {
		panic(err)
	}
	if _, err := os.Stat(roomDir); os.IsNotExist(err) {
		glog.Infof("Making room dir [%s] ...\n", roomDir)
		if err = os.MkdirAll(roomDir, 0755); err != nil {
			panic(err)
		}
	}

	fil := make([]interface{}, 0, 50)
	if files, err := ioutil.ReadDir(roomDir); err != nil {
		panic(err)
	} else {
		for _, file := range files {
			if !file.Mode().IsRegular() {
				continue
			}
			fn := file.Name()
			if strings.ContainsRune(".~!?*", rune(fn[0])) {
				continue // ignore strange file names
			}
			fil = append(fil, []interface{}{
				file.Size(), fn,
			})
		}
	}

	// send back repr for peer to land & receive as obj
	if err = co.SendObj(interop.JSONArray(fil)); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) SendFile(roomID string, fn string) {
	co := chatter.ho.Co()
	// transit the hosting conversation to `send` stage a.s.a.p.
	if err := co.StartSend(); err != nil {
		panic(err)
	}

	roomDir, err := filepath.Abs(fmt.Sprintf("chat-server-files/%s", roomID))
	if err != nil {
		panic(err)
	}

	msg := ""
	fpth := filepath.Join(roomDir, fn)
	if fi, err := os.Stat(fpth); err != nil || !fi.Mode().IsRegular() {
		if !os.IsNotExist(err) {
			glog.Errorf("Error stating file [%s] - %+v - %+v", fpth, fi, err)
		}
		if err = co.SendObj(interop.JSONArray([]interface{}{
			-1, "no such file",
		})); err != nil {
			panic(err)
		}
		return
	} else {
		msg = fmt.Sprintf("last modified: %s", fi.ModTime().Format("2006-01-02 15:04:05"))
	}

	f, err := os.Open(fpth)
	if err != nil {
		if err = co.SendObj(interop.JSONArray([]interface{}{
			-1, fmt.Sprintf("%+v", err),
		})); err != nil {
			panic(err)
		}
		return
	}
	defer f.Close()
	// get file data size
	fsz, err := f.Seek(0, 2)
	if err != nil {
		panic(err)
	}

	if err = co.SendObj(interop.JSONArray([]interface{}{
		fsz, msg,
	})); err != nil {
		panic(err)
	}

	// prepare to send file data from beginning, calculate checksum by the way
	if _, err = f.Seek(0, 0); err != nil {
		panic(err)
	}
	var chksum uint32

	// nothing prevents the file from growing as we're sending, we only send
	// as much as glanced above, so count remaining bytes down,
	// send one 1-KB-chunk at max at a time.
	bytesRemain := fsz
	chunk := make([]byte, 1024) // reused 1 KB buffer
	if err = co.SendStream(func() ([]byte, error) {
		if bytesRemain <= 0 {
			return nil, nil
		}

		if bytesRemain < int64(len(chunk)) {
			chunk = chunk[:bytesRemain]
		}
		if n, err := f.Read(chunk); err != nil {
			if err == io.EOF {
				return nil, errors.New("file shrunk")
			}
			return nil, err
		} else if n < len(chunk) {
			// TODO read in a loop ?
			return nil, errors.New("chunk not fully read")
		}
		bytesRemain -= int64(len(chunk))
		// update chksum
		chksum = crc32.Update(chksum, crc32.IEEETable, chunk)
		return chunk, nil
	}); err != nil {
		return
	}

	// send chksum at last
	if err = co.SendObj(hbi.Repr(chksum)); err != nil {
		panic(err)
	}
}
