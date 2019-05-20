import asyncio
import math
import os.path
import random
import stat
import sys
import time
import traceback
from zlib import crc32

import hbi

from ..ds import *
from ..getline import *
from ..log import *

__all__ = ["Chatter"]


logger = get_logger(__package__)


class Chatter:
    """
    Consumer side chatter object
    
    """

    # name of artifacts to be exposed for peer scripting
    names_to_expose = [
        "ShowNotice",
        "NickChanged",
        "InRoom",
        "RoomMsgs",
        "Said",
        "ChatterJoined",
        "ChatterLeft",
    ]

    def __init__(self, line_getter: GetLine, po: hbi.PostingEnd, ho: hbi.HostingEnd):
        self.line_getter = line_getter
        self.po = po
        self.ho = ho

        self.nick = "?"
        self.in_room = "?"
        self.sent_msgs = []

    async def _set_nick(self, nick: str):

        # showcase the classic request/response pattern of service invocation over HBI wire.

        # start a new posting conversation
        async with self.po.co() as co:  # this async context manager scope programs the
            # `posting stage` of co - a `posting conversation`

            # during the posting stage, send out the nick change request:
            await co.send_code(
                rf"""
SetNick({nick!r})
"""
            )

            # close the posting conversation as soon as all requests are sent,
            # so the wire is released immediately, for rest posting conversaions to start off,
            # with RTT between requests eliminated.

        # once closed, this posting conversation enters `after-posting stage`,
        # a closed po co can do NO sending anymore, but the receiving & processing of response,
        # should be carried out in this stage.

        # execution of current coroutine is actually suspended during `co.recv_obj()`, until
        # the inbound payload matching `co.co_seq` appears on the wire, at which time that
        # payload will be `landed` and the land result will be returned by `co.recv_obj()`.
        # before that, the wire should be busy off loading inbound data corresponding to
        # previous conversations, either posting ones initiated by local peer, or hosting
        # ones triggered by remote peer.

        # receive response within `after-posting stage`:
        accepted_nick = await co.recv_obj()

        # the accepted nick may be moderated, not necessarily the same as requested

        # update local state and TUI, notice the new nick
        self.nick = accepted_nick
        self._update_prompt()
        print(f"You are now known as `{self.nick}`")

    async def _goto_room(self, room_id: str):

        # showcase the idiomatic HBI way of (asynchronous) service call.
        # as the service is invoked, it's at its own discrepancy to back-script this consumer,
        # to change its representatiion states as consequences of the service call. actually
        # that's not only the requesting consumer, but all consumer instances connected to the
        # service, are scripted in realtime response to this particular service call, in largely
        # the same way (asynchronous server-pushing), of state transition to realize the overall
        # system consequences.

        await self.po.notif(
            rf"""
GotoRoom({room_id!r})
"""
        )

    async def _say(self, msg: str):

        # showcase the idiomatic HBI way of (asynchronous) service call, with binary data
        # data following its `receiving-code`, together posted to the service for landing.
        # the service is expected to notify the success-of-display of the message, by
        # back-scripting this consumer to land `Said(msg_id)` during the `after-posting stage`
        # of the implicitly started posting conversation from `po.notif_data()`.

        # record msg to send in local log
        try:
            # try find an empty slot to hold this pending message
            msg_id = self.sent_msgs.index(None)
            self.sent_msgs[msg_id] = msg
        except ValueError:
            # extend a new slot for this pending message
            msg_id = len(self.sent_msgs)
            self.sent_msgs.append(msg)

        # prepare binary data
        msg_buf = msg.encode("utf-8")
        # notif with binary data
        await self.po.notif_data(
            rf"""
Say({msg_id!r}, {len(msg_buf)!r})
""",
            msg_buf,
        )

    async def _list_local_files(self, room_id: str):
        room_dir = os.path.abspath(f"chat-client-files/{room_id}")
        if not os.path.isdir(room_dir):
            print(f"Making room dir [{room_dir}] ...")
            os.makedirs(room_dir, exist_ok=True)

        fnl = []
        for fn in os.listdir(room_dir):
            if fn[0] in ".~!?*":
                continue  # ignore strange file names
            try:
                s = os.stat(os.path.join(room_dir, fn))
            except OSError:
                pass
            if not stat.S_ISREG(s.st_mode):
                continue
            fszkb = int(math.ceil(s.st_size / 1024))
            fnl.append(f"{fszkb:12d} KB\t{fn}")

        print("\n".join(fnl))

    async def _upload_file(self, room_id: str, fn: str):
        room_dir = os.path.abspath(f"chat-client-files/{room_id}")
        if not os.path.isdir(room_dir):
            print(f"Room dir not there: [{room_dir}]")
            return

        fpth = os.path.join(room_dir, fn)
        if not os.path.exists(fpth):
            print(f"File not there: [{fpth}]")
            return
        if not os.path.isfile(fpth):
            print(f"Not a file: [{fpth}]")
            return

        with open(fpth, "rb") as f:
            # get file data size
            f.seek(0, 2)
            fsz = f.tell()

            # prepare to send file data from beginning, calculate checksum by the way
            f.seek(0, 0)
            chksum = 0

            total_kb = int(math.ceil(fsz / 1024))
            print(f" Start uploading {total_kb} KB data ...")

            def stream_file_data():  # a generator function is ideal for binary data streaming
                nonlocal chksum  # this is needed outer side, write to that var

                # nothing prevents the file from growing as we're sending, we only send
                # as much as glanced above, so count remaining bytes down,
                # send one 1-KB-chunk at max at a time.
                bytes_remain = fsz
                while bytes_remain > 0:
                    remain_kb = int(math.ceil(bytes_remain / 1024))
                    print(  # overwrite line above prompt
                        f"\x1B[1A\r\x1B[0K {remain_kb:12d} of {total_kb:12d} KB remaining ..."
                    )

                    chunk = f.read(min(1024, bytes_remain))
                    assert len(chunk) > 0, "file shrunk !?!"

                    yield chunk  # yield it so as to be streamed to server

                    bytes_remain -= len(chunk)
                    chksum = crc32(chunk, chksum)  # update chksum

                assert bytes_remain == 0, "?!"

                # overwrite line above prompt
                print(f"\x1B[1A\r\x1B[0K All {total_kb} KB sent out.")

            async with self.po.co() as co:  # establish a posting conversation for uploading

                # send out receiving-code followed by binary stream
                await co.send_code(
                    rf"""
RecvFile({room_id!r}, {fn!r}, {fsz!r})
"""
                )

                # the implemented solution here is very anti-throughput,
                # the wire is hogged by this conversation for a full network roundtrip,
                # the pipeline will be drained due to blocking wait.
                #
                # but for demonstration purpose, this solution can get the job done at least.
                #
                # a better solution, that's throughput-wise, should be the client submiting an upload
                # intent, and if the service accepts the meta info, it then opens a posting conversation
                # from server side, requests the hosting endpoint of the client to do upload; or in
                # the other case, notify the reason why it's not accepted.
                refuse_reason = await co.recv_obj()
                if refuse_reason is not None:
                    print(f"Server refused the upload: {refuse_reason}")
                    return

                # upload accepted, proceed to upload file data
                start_time = time.monotonic()
                await co.send_data(stream_file_data())

        # receive response AFTER the posting conversation closed,
        # this is crucial for overall throughput.
        # note the file is also closed as earlier as possible.
        peer_chksum = await co.recv_obj()
        elapsed_seconds = time.monotonic() - start_time

        # overwrite line above
        print(
            f"\x1B[1A\r\x1B[0K All {total_kb} KB uploaded in {elapsed_seconds:0.2f} second(s)."
        )
        # validate chksum calculated at peer side as it had all data received
        if peer_chksum != chksum:
            print(f"But checksum mismatch !?!")
        else:
            print(
                rf"""
@@ uploaded {chksum:x} [{fn}]
"""
            )

    async def _list_server_files(self, room_id: str):

        async with self.po.co() as co:  # start a posting conversation

            # send the file listing request
            await co.send_code(
                rf"""
ListFiles({room_id!r})
"""
            )

            # close this posting conversation after all requests sent,
            # so the wire is released immediately,
            # for other posting conversaions to start off,
            # without waiting roundtrip time of this conversation's response.

        # once closed, this posting conversation enters `after-posting stage`,
        # a closed po co can do NO sending anymore, but the receiving & processing of response,
        # should be carried out in this stage.

        fil = await co.recv_obj()

        # show received file info list
        print(
            "\n".join(f"{int(math.ceil(fsz / 1024)):12d} KB\t{fn}" for fsz, fn in fil)
        )

    async def _download_file(self, room_id: str, fn: str):
        room_dir = os.path.abspath(f"chat-client-files/{room_id}")
        if not os.path.isdir(room_dir):
            print(f"Making room dir [{room_dir}] ...")
            os.makedirs(room_dir, exist_ok=True)

        async with self.po.co() as co:  # start a new posting conversation

            # send out download request
            await co.send_code(
                rf"""
SendFile({room_id!r}, {fn!r})
"""
            )

        # receive response AFTER the posting conversation closed,
        # this is crucial for overall throughput.
        fsz, msg = await co.recv_obj()
        if fsz < 0:
            print(f"Server refused file downlaod: {msg}")
            return
        elif msg is not None:
            print(f"@@ Server: {msg}")

        fpth = os.path.join(room_dir, fn)

        # no truncate in case another spammer is racing to upload the same file.
        # concurrent reading and writing to a same file is wrong in most but this spamming case.
        f = os.fdopen(os.open(fpth, os.O_RDWR | os.O_CREAT), "rb+")
        try:
            total_kb = int(math.ceil(fsz / 1024))
            print(f" Start downloading {total_kb} KB data ...")

            # prepare to recv file data from beginning, calculate checksum by the way
            chksum = 0

            def stream_file_data():  # a generator function is ideal for binary data streaming
                nonlocal chksum  # this is needed outer side, write to that var

                # receive 1 KB at most at a time
                buf = bytearray(1024)

                bytes_remain = fsz
                while bytes_remain > 0:

                    if len(buf) > bytes_remain:
                        buf = buf[:bytes_remain]

                    yield buf  # yield it so as to be streamed from client

                    f.write(buf)  # write received data to file

                    bytes_remain -= len(buf)

                    chksum = crc32(buf, chksum)  # update chksum

                    remain_kb = int(math.ceil(bytes_remain / 1024))
                    print(  # overwrite line above prompt
                        f"\x1B[1A\r\x1B[0K {remain_kb:12d} of {total_kb:12d} KB remaining ..."
                    )

                assert bytes_remain == 0, "?!"

                # overwrite line above prompt
                print(f"\x1B[1A\r\x1B[0K All {total_kb} KB received.")

            # receive data stream from server
            start_time = time.monotonic()
            await co.recv_data(stream_file_data())
        finally:
            f.close()

        peer_chksum = await co.recv_obj()
        elapsed_seconds = time.monotonic() - start_time

        # overwrite line above
        print(
            f"\x1B[1A\r\x1B[0K All {total_kb} KB downloaded in {elapsed_seconds:0.2f} second(s)."
        )
        # validate chksum calculated at peer side as it had all data sent
        if peer_chksum != chksum:
            print(f"But checksum mismatch !?!")
        else:
            print(
                rf"""
@@ downloaded {chksum:x} [{fn}]
"""
            )

    async def keep_chatting(self):
        po = self.po

        disc_reason = None
        try:
            while po.is_connected():  # until disconnected from chat service

                sl = await self.line_getter.get_line()
                if sl is None:
                    # User pressed Ctrl^D to end chatting.
                    break

                if len(sl.strip()) < 1:  # only white space(s) or just enter pressed
                    continue

                if sl[0] == "#":
                    # goto the specified room
                    room_id = sl[1:].strip()
                    await self._goto_room(room_id)
                elif sl[0] == "$":
                    # change nick
                    nick = sl[1:].strip()
                    await self._set_nick(nick)
                elif sl[0] == ".":
                    # list local files
                    await self._list_local_files(self.in_room)
                elif sl[0] == "^":
                    # list server files
                    await self._list_server_files(self.in_room)
                elif sl[0] == ">":
                    # upload file
                    fn = sl[1:].strip()
                    await self._upload_file(self.in_room, fn)
                elif sl[0] == "<":
                    # download file
                    fn = sl[1:].strip()
                    await self._download_file(self.in_room, fn)
                elif sl[0] == "*":
                    # spam the service for stress-test
                    spec = sl[1:]
                    await self._spam(spec)
                elif sl[0] == "?":
                    # show usage
                    print(
                        rf"""
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
"""
                    )
                else:
                    msg = sl
                    await self._say(msg)

        except Exception:
            logger.error(f"Failure in chatting.", exc_info=True)
            disc_reason = traceback.print_exc()

        if po.is_connected():
            await po.disconnect(disc_reason)

        print("Bye.")

    async def _spam(self, spec: str):
        fields = [int(f) for f in spec.split()]
        n_bots, n_rooms, n_msgs, n_files, kb_max = (
            fields + [10, 10, 10, 10, 1234][len(fields) :]
        )

        if kb_max > 0:
            print(
                rf"""
Start spamming with {n_bots} bots in up to {n_rooms} rooms,
  each to speak up to {n_msgs} messages,
  and upload/download up to {n_files} files, each up to {kb_max} KB large ...

"""
            )
        else:
            print(
                rf"""
Start spamming with {n_bots} bots in up to {n_rooms} rooms,
  each to speak up to {n_msgs} messages,
  and download up to {n_files} files ...

"""
            )

        async def bot_spam(idSpammer: str):
            for i_room in range(n_rooms):
                id_room = f"Spammed{1+i_room}"
                room_dir = os.path.abspath(f"chat-client-files/{id_room}")
                os.makedirs(room_dir, exist_ok=True)

                for i_msg in range(n_msgs):

                    await self._goto_room(id_room)
                    await self._set_nick(idSpammer)
                    await self._say(f"This is {idSpammer} giving you {1+i_msg} !")

                for i_file in range(n_files):

                    fn = f"SpamFile{1+i_file}"
                    # 25% probability to do download, 75% do upload
                    do_upload = kb_max > 0 and random.randint(0, 3) > 0

                    if do_upload:

                        # generate file if not present
                        fpth = os.path.join(room_dir, fn)
                        if not os.path.exists(fpth):
                            # no truncate in case another spammer is racing to write the same file.
                            # concurrent writing to a same file is wrong in most but this spamming case.
                            f = os.fdopen(os.open(fpth, os.O_RDWR | os.O_CREAT), "rb+")
                            try:
                                kb_file = random.randint(1, kb_max)
                                f.seek(0, 2)
                                existing_fsz = f.tell()

                                # only write when file is not big enough
                                if existing_fsz < 1024 * kb_file:
                                    f.seek(0, 0)
                                    for i in range(kb_file):
                                        f.write(
                                            random.getrandbits(8 * 1024).to_bytes(
                                                1024, sys.byteorder
                                            )
                                        )
                            finally:
                                f.close()

                        await self._goto_room(id_room)
                        await self._set_nick(idSpammer)
                        await self._upload_file(id_room, fn)

                    else:

                        await self._download_file(id_room, fn)

        random.seed()
        for done_spamming in asyncio.as_completed(
            [
                asyncio.create_task(bot_spam(f"Spammer{1+i_bot }"))
                for i_bot in range(n_bots)
            ]
        ):
            await done_spamming  # re-raise its exception if any

        if kb_max > 0:
            print(
                rf"""
Spammed with {n_bots} bots in up to {n_rooms} rooms,
  each to speak up to {n_msgs} messages,
  and upload/download up to {n_files} files, each up to {kb_max} KB large.

"""
            )
        else:
            print(
                rf"""
Spammed with {n_bots} bots in up to {n_rooms} rooms,
  each to speak up to {n_msgs} messages,
  and download up to {n_files} files.

"""
            )

    def _update_prompt(self):
        self.line_getter.ps1 = f"{self.nick!s}@{self.po.remote_addr!s}#{self.in_room}: "

    def NickChanged(self, nick: str):
        self.nick = nick
        self._update_prompt()

    def InRoom(self, room_id: str):
        self.in_room = room_id
        self._update_prompt()

    def RoomMsgs(self, room_msgs: MsgsInRoom):
        if room_msgs.room_id != self.in_room:
            self.line_getter.show(f" *** Messages from #{room_msgs.room_id!s} ***")
        self.line_getter.show("\n".join(str(msg) for msg in room_msgs.msgs))

    def Said(self, msg_id: int):
        msg = self.sent_msgs[msg_id]
        self.line_getter.show(
            f"@@ Your message [{msg_id!s}] has been displayed:\n  > {msg!s}"
        )
        self.sent_msgs[msg_id] = None

    def ShowNotice(self, text: str):
        self.line_getter.show(text)

    def ChatterJoined(self, nick: str, room_id: str):
        self.line_getter.show(f"@@ {nick!s} has joined #{room_id!s}")

    def ChatterLeft(self, nick: str, room_id: str):
        self.line_getter.show(f"@@ {nick!s} has left #{room_id!s}")
