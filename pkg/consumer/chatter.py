import asyncio
import math
import os.path
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

        async with self.po.co() as co:  # start a posting conversation

            # send the nick change request
            await co.send_code(
                rf"""
SetNick({nick!r})
"""
            )

            # close this posting conversation after all requests sent,
            # so the wire is released immediately,
            # for other posting conversaions to start sending,
            # without waiting roundtrip of this conversation's response.

        # a closed conversation can do NO sending anymore,
        # but the receiving of response is very much prefered to be
        # carried out after it's closed.
        # this is crucial for overall throughput with the underlying HBI wire.
        accepted_nick = await co.recv_obj()

        # the accepted nick may be moderated, not necessarily the same as requested

        # update local state and TUI, notice the new nick
        self.nick = accepted_nick
        self._update_prompt()
        self.line_getter.show(f"You are now known as `{self.nick}`")

    async def _goto_room(self, room_id: str):
        await self.po.notif(
            rf"""
GotoRoom({room_id!r})
"""
        )

    async def _say(self, msg: str):
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
        # showcase notif with binary payload
        await self.po.notif_data(
            rf"""
Say({msg_id!r}, {len(msg_buf)!r})
""",
            msg_buf,
        )

    async def _list_local_files(self):
        room_dir = os.path.abspath(f"room-files/{self.in_room}")
        if not os.path.isdir(room_dir):
            self.line_getter.show(f"Making room dir [{room_dir}] ...")
            os.makedirs(room_dir, exist_ok=True)

        fnl = []
        for fn in os.listdir(room_dir):
            if fn[0] in ".~!?*":
                continue # ignore strange file names
            try:
                s = os.stat(os.path.join(room_dir, fn))
            except OSError:
                pass
            fszkb = int(math.ceil(s.st_size / 1024))
            fnl.append(f"{fszkb:12d} KB\t{fn}")

        self.line_getter.show("\n".join(fnl))

    async def _upload_file(self, fn):
        lg = self.line_getter

        room_dir = os.path.abspath(f"room-files/{self.in_room}")
        if not os.path.isdir(room_dir):
            lg.show(f"Room dir not there: [{room_dir}]")
            return

        fpth = os.path.join(room_dir, fn)
        if not os.path.exists(fpth):
            lg.show(f"File not there: [{fpth}]")
            return
        if not os.path.isfile(fpth):
            lg.show(f"Not a file: [{fpth}]")
            return

        start_time = time.monotonic()
        with open(fpth, "rb") as f:
            # get file data size
            f.seek(0, 2)
            fsz = f.tell()
            total_kb = int(math.ceil(fsz / 1024))
            lg.show(f" Start uploading {total_kb} KB data ...")

            # prepare to send file data from beginning, calculate checksum by the way
            f.seek(0, 0)
            chksum = 0

            def stream_file_data():  # a generator function is ideal for binary data streaming
                nonlocal chksum  # this is needed outer side, write to that var

                # nothing prevents the file from growing as we're sending, we only send
                # as much as glanced above, so count remaining bytes down,
                # send one 1-KB-chunk at max at a time.
                bytes_remain = fsz
                while bytes_remain > 0:
                    chunk = f.read(min(1024, bytes_remain))
                    assert len(chunk) > 0, "file shrunk !?!"
                    bytes_remain -= len(chunk)

                    yield chunk  # yield it so as to be streamed to server
                    chksum = crc32(chunk, chksum)  # update chksum

                    remain_kb = int(math.ceil(bytes_remain / 1024))
                    lg.show(  # overwrite line above prompt
                        f"\x1B[1A\r\x1B[0K {remain_kb:12d} of {total_kb:12d} KB remaining ..."
                    )
                assert bytes_remain == 0, "?!"

                # overwrite line above prompt
                lg.show(f"\x1B[1A\r\x1B[0K All {total_kb} KB sent out.")

            async with self.po.co() as co:  # establish a posting conversation for uploading

                # send out receiving-code followed by binary stream
                await co.send_code(
                    rf"""
RecvFile({self.in_room!r}, {fn!r}, {fsz!r})
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
                    lg.show(f"Server refused the upload: {refuse_reason}")
                    return

                # upload accepted, proceed to upload file data
                await co.send_data(stream_file_data())

        # receive response AFTER the posting conversation closed,
        # this is crucial for overall throughput.
        # note the file is also closed as earlier as possible.
        peer_chksum = await co.recv_obj()
        elapsed_seconds = time.monotonic() - start_time

        # overwrite line above
        lg.show(
            f"\x1B[1A\r\x1B[0K All {total_kb} KB uploaded in {elapsed_seconds:0.2f} second(s)."
        )
        # validate chksum calculated at peer side as it had all data received
        if peer_chksum != chksum:
            lg.show(f"But checksum mismatch !?!")
        else:
            lg.show(
                rf"""
@@ uploaded {chksum:x} [{fn}]
"""
            )

    async def _list_server_files(self):

        async with self.po.co() as co:  # start a posting conversation

            # send the file listing request
            await co.send_code(
                rf"""
ListFiles({self.in_room!r})
"""
            )

            # close this posting conversation after all requests sent,
            # so the wire is released immediately,
            # for other posting conversaions to start sending,
            # without waiting roundtrip of this conversation's response.

        # a closed conversation can do NO sending anymore,
        # but the receiving of response is very much prefered to be
        # carried out after it's closed.
        # this is crucial for overall throughput with the underlying HBI wire.
        fil = await co.recv_obj()

        # show received file info list
        self.line_getter.show(
            "\n".join(f"{int(math.ceil(fsz / 1024)):12d} KB\t{fn}" for fsz, fn in fil)
        )

    async def _download_file(self, fn):
        lg = self.line_getter

        room_dir = os.path.abspath(f"room-files/{self.in_room}")
        if not os.path.isdir(room_dir):
            self.line_getter.show(f"Making room dir [{room_dir}] ...")
            os.makedirs(room_dir, exist_ok=True)

        async with self.po.co() as co:  # start a posting conversation

            await co.send_code(
                rf"""
SendFile({self.in_room!r}, {fn!r})
"""
            )

        # receive response AFTER the posting conversation closed,
        # this is crucial for overall throughput.
        fsz, msg = await co.recv_obj()
        if fsz < 0:
            lg.show(f"Server refused file downlaod: {msg}")
            return
        elif msg is not None:
            lg.show(f"@@ Server: {msg}")

        fpth = os.path.join(room_dir, fn)

        start_time = time.monotonic()
        with open(fpth, "wb") as f:
            total_kb = int(math.ceil(fsz / 1024))
            lg.show(f" Start downloading {total_kb} KB data ...")

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
                    lg.show(  # overwrite line above prompt
                        f"\x1B[1A\r\x1B[0K {remain_kb:12d} of {total_kb:12d} KB remaining ..."
                    )

                assert bytes_remain == 0, "?!"

                # overwrite line above prompt
                lg.show(f"\x1B[1A\r\x1B[0K All {total_kb} KB received.")

            # receive data stream from server
            await co.recv_data(stream_file_data())

        peer_chksum = await co.recv_obj()
        elapsed_seconds = time.monotonic() - start_time

        # overwrite line above
        lg.show(
            f"\x1B[1A\r\x1B[0K All {total_kb} KB downloaded in {elapsed_seconds:0.2f} second(s)."
        )
        # validate chksum calculated at peer side as it had all data sent
        if peer_chksum != chksum:
            lg.show(f"But checksum mismatch !?!")
        else:
            lg.show(
                rf"""
@@ downloaded {chksum:x} [{fn}]
"""
            )

    async def keep_chatting(self):

        hbic = self.po.hbic
        disc_reason = None
        try:
            while hbic.is_connected():  # until disconnected from chat service

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
                    await self._list_local_files()
                elif sl[0] == "^":
                    # list server files
                    await self._list_server_files()
                elif sl[0] == ">":
                    # upload file
                    fn = sl[1:].strip()
                    await self._upload_file(fn)
                elif sl[0] == "<":
                    # download file
                    fn = sl[1:].strip()
                    await self._download_file(fn)
                elif sl[0] == "?":
                    # show usage
                    self.line_getter.show(
                        rf"""
Usage:

  * #`room`
    goto a room

  * $`nick`
    change nick

  * .
    list local files

  * ^
    list server files

  * >`file-name`
    upload a file

  * <`file-name`
    download a file
"""
                    )
                else:
                    msg = sl
                    await self._say(msg)

        except Exception:
            logger.error(f"Failure in chatting.", exc_info=True)
            disc_reason = traceback.print_exc()

        if hbic.is_connected():
            await hbic.disconnect(disc_reason)

        print("Bye.")

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
