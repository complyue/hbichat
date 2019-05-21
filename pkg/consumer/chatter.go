package consumer

import (
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
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
	fmt.Printf("Your are now known as `%s`\n", acceptedNick)
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

func (chatter *Chatter) listLocalFiles(roomID string) {
	roomDir, err := filepath.Abs(fmt.Sprintf("chat-client-files/%s", roomID))
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

func (chatter *Chatter) uploadFile(roomID, fn string) {
	roomDir, err := filepath.Abs(fmt.Sprintf("chat-client-files/%s", roomID))
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

	// start a new posting conversation for upload request
	co, err := chatter.po.NewCo()
	if err != nil {
		panic(err)
	}
	func() {
		defer co.Close() // close this po co for sure, on leaving this one-off func

		// submit an upload request
		if err = co.SendCode(
			fmt.Sprintf(`
UploadReq(%#v, %#v, %d)
`, roomID, fn, fsz)); err != nil {
			panic(err)
		}
	}()

	// after the co closed,  i.e. in its `recv` phase, receive upload confirmation
	if refuseReason, err := co.RecvObj(); err != nil {
		panic(err)
	} else if refuseReason != nil {
		fmt.Printf("Server refused the upload: %s\n", refuseReason)
		return
	}

	// prepare to send file data from beginning, calculate checksum by the way
	if _, err = f.Seek(0, 0); err != nil {
		panic(err)
	}
	var chksum uint32

	// these need to be accessed both inside and outside of data stream cb, define here
	var startTime time.Time
	totalKB := int64(math.Ceil(float64(fsz) / 1024))
	fmt.Printf(" Start uploading %d KB data ...\n", totalKB)

	// start another posting conversation for file data upload
	co, err = chatter.po.NewCo()
	if err != nil {
		panic(err)
	}
	func() {
		defer co.Close() // close this po co for sure, on leaving this one-off func

		// send out receiving-code followed by binary stream
		if err = co.SendCode(
			fmt.Sprintf(`
RecvFile(%#v, %#v, %d)
`, roomID, fn, fsz)); err != nil {
			panic(err)
		}

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

	// after the co closed,  i.e. in its `recv` phase, receive the checksum calculated
	// as peer received the data stream.
	peerChksum, err := co.RecvObj()
	if err != nil {
		panic(err)
	}
	elapsed := time.Since(startTime)
	fmt.Printf( // overwrite line above
		"\x1B[1A\r\x1B[0K All %d KB uploaded in %v\n", totalKB, elapsed)
	// validate chksum calculated at peer side against ours.
	// use hex string form so don't depend on its exact type (int64 or int etc.) as Anko
	// interpreted it.
	if fmt.Sprintf("%x", peerChksum) != fmt.Sprintf("%x", chksum) {
		fmt.Printf("@*@ But checksum mismatch %x vs %x !?!\n", peerChksum, chksum)
	} else {
		fmt.Printf(`
@@ uploaded %x [%s]
`, chksum, fn)
	}
}

func (chatter *Chatter) listServerFiles(roomID string) {
	co, err := chatter.po.NewCo()
	if err != nil {
		panic(err)
	}
	func() {
		defer co.Close()

		// send the file listing request
		if err := co.SendCode(fmt.Sprintf(`
ListFiles(%#v)
`, roomID)); err != nil {
			panic(err)
		}

		// close this posting conversation after all requests sent,
		// so the wire is released immediately,
		// for other posting conversaions to start off,
		// without waiting roundtrip time of this conversation's response.
	}()

	// once closed, this posting conversation enters `after-posting stage`,
	// a closed po co can do NO sending anymore, but the receiving & processing of response,
	// should be carried out in this stage.

	fil, err := co.RecvObj()
	if err != nil {
		panic(err)
	}

	// show received file info list
	for _, fi := range fil.([]interface{}) {
		fsz, fn := fi.([]interface{})[0].(int64), fi.([]interface{})[1].(string)
		fmt.Printf("%12d KB\t%s\n", int(math.Ceil(float64(fsz)/1024)), fn)
	}
}

func (chatter *Chatter) downloadFile(roomID, fn string) {
	roomDir, err := filepath.Abs(fmt.Sprintf("chat-client-files/%s", roomID))
	if err != nil {
		panic(err)
	}
	if err = os.MkdirAll(roomDir, 0755); err != nil {
		panic(err)
	}

	// start a new posting conversation
	co, err := chatter.po.NewCo()
	if err != nil {
		panic(err)
	}
	func() {
		defer co.Close() // close this po co for sure, on leaving this one-off func

		// send out download request
		if err = co.SendCode(
			fmt.Sprintf(`
SendFile(%#v, %#v)
`, roomID, fn)); err != nil {
			panic(err)
		}
	}()

	// receive response AFTER the posting conversation closed,
	// this is crucial for overall throughput.
	dldResp, err := co.RecvObj()
	if err != nil {
		panic(err)
	}
	fsz, msg := dldResp.([]interface{})[0].(int64), dldResp.([]interface{})[1]
	if fsz < 0 {
		fmt.Printf("Server refused file download: %s\n", msg)
		return
	} else if msg != nil {
		fmt.Printf("@@ Server: %s\n", msg)
	}

	fpth := filepath.Join(roomDir, fn)
	// unlink the file before creating a new one, so if someone has opened it for
	// upload, that can finish normally.
	os.Remove(fpth)
	f, err := os.Create(fpth)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	// prepare to recv file data from beginning, calculate checksum by the way
	var chksum uint32

	totalKB := int64(math.Ceil(float64(fsz) / 1024))
	fmt.Printf(" Start downloading %d KB data ...\n", totalKB)

	// receive data stream from server
	startTime := time.Now()
	bytesRemain := fsz
	chunk := make([]byte, 1024) // reused 1 KB buffer
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

		if bytesRemain <= 0 {
			fmt.Printf( // overwrite line above prompt
				"\x1B[1A\r\x1B[0K All %12d KB received.\n", totalKB,
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
		bytesRemain -= int64(len(chunk))
		return chunk, nil
	}); err != nil {
		return
	}

	peerChksum, err := co.RecvObj()
	if err != nil {
		panic(err)
	}
	elapsed := time.Since(startTime)
	fmt.Printf( // overwrite line above
		"\x1B[1A\r\x1B[0K All %d KB downloaded in %v\n", totalKB, elapsed)
	// validate chksum calculated at peer side as it had all data sent
	if fmt.Sprintf("%x", peerChksum) != fmt.Sprintf("%x", chksum) {
		fmt.Printf("@*@ But checksum mismatch %x vs %x !?!\n", peerChksum, chksum)
	} else {
		fmt.Printf(`
@@ downloaded %x [%s]
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
			chatter.listLocalFiles(chatter.inRoom)
		} else if code[0] == '^' {
			// list server files
			chatter.listServerFiles(chatter.inRoom)
		} else if code[0] == '>' {
			// upload file
			chatter.uploadFile(chatter.inRoom, strings.TrimSpace(code[1:]))
		} else if code[0] == '<' {
			// download file
			chatter.downloadFile(chatter.inRoom, strings.TrimSpace(code[1:]))
		} else if code[0] == '*' {
			// spam the service for stress-test
			chatter.spam(code[1:])
		} else if code[0] == '?' {
			// show usage
			fmt.Print(`
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

 * [ _n_bots_=10 ] [ _n_rooms_=10 ] [ _n_msgs_=10 ] [ _n_files_=10 ] [ _file_max_kb_=1234 ]
    spam the service for stress-test

`)
		} else {
			msg := code
			chatter.say(msg)
		}
	}

}

func (chatter *Chatter) spam(spec string) {
	nBots, nRooms, nMsgs, nFiles, kbMax := 10, 10, 10, 10, 1234
	fmt.Sscanf(spec, "%d %d %d %d %d", &nBots, &nRooms, &nMsgs, &nFiles, &kbMax)
	if kbMax > 0 {
		fmt.Printf(`
Start spamming with %d bots in up to %d rooms,
  each to speak up to %d messages,
  and upload/download up to %d files, each up to %d KB large ...

`,
			nBots, nRooms, nMsgs, nFiles, kbMax)
	} else {
		fmt.Printf(`
Start spamming with %d bots in up to %d rooms,
	each to speak up to %d messages,
	and download up to %d files ...

`,
			nBots, nRooms, nMsgs, nFiles)
	}

	rand.Seed(time.Now().UnixNano())
	var waitBots sync.WaitGroup
	for iBot := range make([]struct{}, nBots) {
		waitBots.Add(1)
		go func(idSpammer string) {
			defer waitBots.Done()

			for iRoom := range make([]struct{}, (1 + rand.Intn(nRooms))) {
				idRoom := fmt.Sprintf("Spammed%d", 1+iRoom)
				roomDir, err := filepath.Abs(fmt.Sprintf("chat-client-files/%s", idRoom))
				if err = os.MkdirAll(roomDir, 0755); err != nil {
					panic(err)
				}

				for iMsg := range make([]struct{}, (1 + rand.Intn(nMsgs))) {

					chatter.gotoRoom(idRoom)
					chatter.setNick(idSpammer)
					chatter.say(fmt.Sprintf("This is %s giving you %d !", idSpammer, 1+iMsg))

				}

				for iFile := range make([]struct{}, (1 + rand.Intn(nFiles))) {

					fn := fmt.Sprintf("SpamFile%d", (1 + iFile))
					// 25% probability to do download, 75% do upload
					doUpload := kbMax > 0 && rand.Intn(4) > 0
					if doUpload {

						// generate file if not present
						fpth := filepath.Join(roomDir, fn)
						if _, err := os.Stat(fpth); os.IsNotExist(err) {
							func() {
								// no truncate in case another spammer is racing to write the same file.
								// concurrent writing to a same file is wrong in most real world cases,
								// but here we're just spamming ...
								f, err := os.OpenFile(fpth, os.O_RDWR|os.O_CREATE, 0666)
								if err != nil {
									panic(err)
								}
								defer f.Close()

								kbFile := 1 + rand.Intn(kbMax)
								if existingSize, err := f.Seek(0, 2); err != nil {
									panic(err)
								} else if existingSize >= 1024*int64(kbFile) {
									// already big enough
									return
								}
								if _, err := f.Seek(0, 0); err != nil { // reset to beginning for write
									panic(err)
								}

								chunk := make([]byte, 1024) // reused 1 KB buffer
								for range make([]struct{}, kbFile) {
									if _, err = rand.Read(chunk); err != nil {
										panic(err)
									}
									if _, err = f.Write(chunk); err != nil {
										panic(err)
									}
								}
							}()
						}

						chatter.gotoRoom(idRoom)
						chatter.setNick(idSpammer)
						chatter.uploadFile(idRoom, fn)

					} else {

						chatter.downloadFile(idRoom, fn)

					}

				}

			}

		}(fmt.Sprintf("Spammer%d", 1+iBot))
	}

	waitBots.Wait()
	if kbMax > 0 {
		fmt.Printf(`
Spammed with %d bots in up to %d rooms,
  each to speak up to %d messages,
  and upload/download up to %d files, each up to %d KB large.

`,
			nBots, nRooms, nMsgs, nFiles, kbMax)
	} else {
		fmt.Printf(`
Spammed with %d bots in up to %d rooms,
	each to speak up to %d messages,
	and download up to %d files.

`,
			nBots, nRooms, nMsgs, nFiles)
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
		fmt.Printf(" *** Messages in #%s ***\n", roomMsgs.RoomID)
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
