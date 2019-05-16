package consumer

import (
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/complyue/hbi"
	"github.com/complyue/hbi/interop"
	"github.com/complyue/hbi/pkg/errors"
	"github.com/complyue/hbichat/pkg/ds"
	"github.com/complyue/liner"
	"github.com/golang/glog"
)

// process global liner
var line *liner.State

func init() {
	line = liner.NewLiner()
}

func Cleanup() {

	line.Close()

}

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

	he.ExposeFunction("__hbi_init__", func(po *hbi.PostingEnd, ho *hbi.HostingEnd) {
		serviceAddr = fmt.Sprintf("%s", po.RemoteAddr())

		chatter = &Chatter{
			po: po, ho: ho,
			nick: "?", inRoom: "?",
			prompt: fmt.Sprintf(">%s> ", serviceAddr),
		}

		he.ExposeReactor(chatter)

		go func() {
			defer func() {
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

	he.ExposeFunction("__hbi_cleanup__", func(discReason string) {

		if glog.V(1) {
			glog.Infof("Connection to chatting service %s lost: %s", serviceAddr, discReason)
		}

	})

	return he
}

// Chatter defines consumer side chatter object
type Chatter struct {
	po *hbi.PostingEnd
	ho *hbi.HostingEnd

	nick     string
	inRoom   string
	sentMsgs []string

	prompt string

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
		"ShowNotice",
		"NickChanged",
		"InRoom",
		"RoomMsgs",
		"Said",
		"ChatterJoined",
		"ChatterLeft",
	}
}

func (chatter *Chatter) setNick(nick string) {

	// showcase the classic request/response pattern of service invocation over HBI wire.

	// start a new posting conversation
	co, err := chatter.po.NewCo()
	if err != nil {
		panic(err)
	}
	func() { // this one-off, immediately-called, anonymous function, programs the
		// `posting stage` of co - a `posting conversation`

		defer co.Close() // close this po co for sure, on leaving this one-off func

		// during the posting stage, send out the nick change request:
		if err = co.SendCode(
			fmt.Sprintf(`
SetNick(%#v)
`, nick)); err != nil {
			panic(err)
		}

		// close the posting conversation as soon as all requests are sent,
		// so the wire is released immediately, for rest posting conversaions to start off,
		// with RTT between requests eliminated.
	}()

	// once closed, this posting conversation enters `after-posting stage`,
	// a closed po co can do NO sending anymore, but the receiving of response, as in the
	// classic pattern, needs to be received and processed.

	// execution of current goroutine is actually suspended during `co.RecvObj()`, until
	// the inbound payload matching `co.CoSeq()` appears on the wire, at which time that
	// payload will be `landed` and the land result will be returned by `co.RecvObj()`.
	// before that, the wire should be busy off loading inbound data corresponding to
	// previous conversations, either posting ones initiated by local peer, or hosting
	// ones triggered by remote peer.

	// receive response within `after-posting stage`:
	acceptedNick, err := co.RecvObj()
	if err != nil {
		panic(err)
	}

	// the accepted nick may be moderated, not necessarily the same as requested

	// update local state and TUI, notice the new nick
	chatter.nick = acceptedNick.(string)
	chatter.updatePrompt()
	line.HidePrompt()
	fmt.Printf("Your are now known as `%s`\n", acceptedNick)
	line.ShowPrompt()
}

func (chatter *Chatter) gotoRoom(roomID string) {

	// showcase the idiomatic HBI way of (asynchronous) service call.
	// as the service is invoked, it's at its own discrepancy to back-script this consumer,
	// to change its representatiion states as consequences of the service call. actually
	// that's not only the requesting consumer, but all consumer instances connected to the
	// service, are scripted in realtime response to this particular service call, in largely
	// the same way (asynchronous server-pushing), of state transition to realize the overall
	// system consequences.

	if err := chatter.po.Notif(fmt.Sprintf(`
GotoRoom(%#v)
`, roomID)); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) say(msg string) {

	// showcase the idiomatic HBI way of (asynchronous) service call, with binary data
	// data following its `receiving-code`, together posted to the service for landing.
	// the service is expected to notify the success-of-display of the message, by
	// back-scripting this consumer to land `Said(msg_id)` during the `after-posting stage`
	// of the implicitly started posting conversation from `po.notif_data()`.

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
		chatter.mu.Lock()
		chatter.sentMsgs = append(chatter.sentMsgs, msg)
		chatter.mu.Unlock()
	} else {
		chatter.mu.Lock()
		chatter.sentMsgs[msgID] = msg
		chatter.mu.Unlock()
	}

	// prepare binary data
	msgBuf := []byte(msg)
	// notif with binary payload
	if err := chatter.po.NotifData(fmt.Sprintf(`
Say(%d, %d)
`, msgID, len(msgBuf)), msgBuf); err != nil {
		panic(err)
	}
}

func (chatter *Chatter) listLocalFiles() {
	line.HidePrompt()
	defer line.ShowPrompt()

	roomDir, err := filepath.Abs(fmt.Sprintf("chat-client-files/%s", chatter.inRoom))
	if err != nil {
		panic(err)
	}
	if fi, err := os.Stat(roomDir); os.IsNotExist(err) {
		fmt.Printf("Making room dir [%s] ...\n", roomDir)
		if err = os.MkdirAll(roomDir, 0755); err != nil {
			panic(err)
		}
	} else if !fi.Mode().IsDir() {
		panic(errors.Errorf("not a dir: %s", roomDir))
	}

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
			fmt.Printf("%12d KB\t%s\n", int64(math.Ceil(float64(file.Size())/1024)), fn)
		}
	}
}

func (chatter *Chatter) uploadFile(fn string) {
	roomDir, err := filepath.Abs(fmt.Sprintf("chat-client-files/%s", chatter.inRoom))
	if err != nil {
		panic(err)
	}
	if _, err := os.Stat(roomDir); os.IsNotExist(err) {
		fmt.Printf("Room dir not there: [%s]\n", roomDir)
		return
	}

	fpth := filepath.Join(roomDir, fn)
	if fi, err := os.Stat(fpth); os.IsNotExist(err) {
		fmt.Printf("File not there: [%s]\n", fpth)
		return
	} else if !fi.Mode().IsRegular() {
		fmt.Printf("Not a file: [%s]\n", fpth)
		return
	}

	f, err := os.Open(fpth)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	// get file data size
	fsz, err := f.Seek(0, 2)
	if err != nil {
		panic(err)
	}

	// these need to be accessed both inside and outside of data stream cb, define here
	totalKB := int64(math.Ceil(float64(fsz) / 1024))
	var startTime time.Time

	// prepare to send file data from beginning, calculate checksum by the way
	if _, err = f.Seek(0, 0); err != nil {
		panic(err)
	}
	var chksum uint32

	// start a new posting conversation
	co, err := chatter.po.NewCo()
	if err != nil {
		panic(err)
	}
	uploadAccepted := false
	func() {
		defer co.Close() // close this po co for sure, on leaving this one-off func

		// send out receiving-code followed by binary stream
		if err = co.SendCode(
			fmt.Sprintf(`
RecvFile(%#v, %#v, %d)
`, chatter.inRoom, fn, fsz)); err != nil {
			panic(err)
		}

		// the implemented solution here is very anti-throughput,
		// the wire is hogged by this conversation for a full network roundtrip,
		// the pipeline will be drained due to blocking wait.
		//
		// but for demonstration purpose, this solution can get the job done at least.
		//
		// a better solution, that's throughput-wise, should be the client submiting an upload
		// intent, and if the service accepts the meta info, it then opens a posting conversation
		// from server side, requests the hosting endpoint of the client to do upload; or in
		// the other case, notify the reason why it's not accepted.
		if refuseReason, err := co.RecvObj(); err != nil {
			panic(err)
		} else if refuseReason != nil {
			fmt.Printf("Server refused the upload: %s\n", refuseReason)
			return
		}

		// upload accepted, proceed to upload file data
		uploadAccepted = true
		fmt.Printf(" Start uploading %d KB data ...\n", totalKB)
		startTime = time.Now()

		// nothing prevents the file from growing as we're sending, we only send
		// as much as glanced above, so count remaining bytes down,
		// send one 1-KB-chunk at max at a time.
		bytesRemain := fsz
		chunk := make([]byte, 1024) // reused 1 KB buffer
		if err = co.SendStream(func() ([]byte, error) {
			if bytesRemain <= 0 {
				fmt.Printf( // overwrite line above prompt
					"\x1B[1A\r\x1B[0K All %12d KB sent out.\n", totalKB,
				)
				return nil, nil
			}

			remainKB := int64(math.Ceil(float64(bytesRemain) / 1024))
			fmt.Printf( // overwrite line above prompt
				"\x1B[1A\r\x1B[0K %12d of %12d KB remaining ...\n", remainKB, totalKB,
			)

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
	}()
	if !uploadAccepted {
		return
	}

	// receive response AFTER the posting conversation closed,
	// this is crucial for overall throughput.
	peerChksum, err := co.RecvObj()
	if err != nil {
		panic(err)
	}
	elapsed := time.Since(startTime)
	fmt.Printf( // overwrite line above
		"\x1B[1A\r\x1B[0K All %d KB uploaded in %v\n", totalKB, elapsed)
	// validate chksum calculated at peer side as it had all data received
	if fmt.Sprintf("%x", peerChksum) != fmt.Sprintf("%x", chksum) {
		fmt.Printf("@*@ But checksum mismatch %x vs %x !?!\n", peerChksum, chksum)
	} else {
		fmt.Printf(`
@@ uploaded %x [%s]
`, chksum, fn)
	}
}

func (chatter *Chatter) keepChatting() {

	for {
		select {
		case <-chatter.po.Done(): // disconnected from chat service
			return
		default: // still connected
		}

		code, err := line.Prompt(chatter.prompt)
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
		line.AppendHistory(code)

		if code[0] == '#' {
			// goto the specified room
			roomID := strings.TrimSpace(code[1:])
			chatter.gotoRoom(roomID)
		} else if code[0] == '$' {
			// change nick
			nick := strings.TrimSpace(code[1:])
			chatter.setNick(nick)
		} else if code[0] == '.' {
			// list local files
			chatter.listLocalFiles()
		} else if code[0] == '^' {
			// list server files
		} else if code[0] == '>' {
			// upload file
			chatter.uploadFile(strings.TrimSpace(code[1:]))
		} else if code[0] == '<' {
			// download file
		} else if code[0] == '?' {
			// show usage
			line.HidePrompt()
			fmt.Println(`
Usage:

 # _room_
    goto a room

 $ _nick_
    change nick

 . 
    list local files

 ^ 
    list server files

 > _file-name_
    upload a file

 < _file-name_
    download a file
`)
			line.ShowPrompt()
		} else {
			msg := code
			chatter.say(msg)
		}
	}

}

func (chatter *Chatter) updatePrompt() {
	chatter.prompt = fmt.Sprintf("%s@%s#%s: ", chatter.nick, chatter.po.RemoteAddr(), chatter.inRoom)
	line.ChangePrompt(chatter.prompt)
}

func (chatter *Chatter) NickChanged(nick string) {
	chatter.mu.Lock()
	defer chatter.mu.Unlock()

	chatter.nick = nick
	chatter.updatePrompt()
}

func (chatter *Chatter) InRoom(roomID string) {
	chatter.mu.Lock()
	defer chatter.mu.Unlock()

	chatter.inRoom = roomID
	chatter.updatePrompt()
}

func (chatter *Chatter) RoomMsgs(roomMsgs *ds.MsgsInRoom) {
	line.HidePrompt()
	if roomMsgs.RoomID != chatter.inRoom {
		fmt.Printf(" *** Messages from #%s ***\n", roomMsgs.RoomID)
	}
	for i := range roomMsgs.Msgs {
		fmt.Printf("%+v\n", &roomMsgs.Msgs[i])
	}
	line.ShowPrompt()
}

func (chatter *Chatter) Said(msgID int) {
	chatter.mu.Lock()
	msg := chatter.sentMsgs[msgID]
	chatter.sentMsgs[msgID] = ""
	chatter.mu.Unlock()

	line.HidePrompt()
	fmt.Printf("@@ Your message [%d] has been displayed:\n  > %s\n", msgID, msg)
	line.ShowPrompt()
}

func (chatter *Chatter) ShowNotice(text string) {
	line.HidePrompt()
	fmt.Println(text)
	line.ShowPrompt()
}

func (chatter *Chatter) ChatterJoined(nick string, roomID string) {
	line.HidePrompt()
	fmt.Printf("@@ %s has joined #%s\n", nick, roomID)
	line.ShowPrompt()
}

func (chatter *Chatter) ChatterLeft(nick string, roomID string) {
	line.HidePrompt()
	fmt.Printf("@@ %s has left #%s\n", nick, roomID)
	line.ShowPrompt()
}
