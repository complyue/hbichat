import asyncio
from datetime import datetime
import math
import os.path
import time
import traceback
from collections import deque
from zlib import crc32

from hbi import *

from ..ds import *
from ..log import *
from .room import *

__all__ = ["Chatter"]

logger = get_logger(__package__)


rooms = {}


def prepare_room(room_id: str = None):
    if not room_id:
        room_id = "Lobby"
    room = rooms.get(room_id, None)
    if room is None:
        room = rooms[room_id] = Room(room_id)
    return room


class Chatter:
    """
    Server side chatter object
    
    """

    # name of artifacts to be exposed for peer scripting
    names_to_expose = [
        "SetNick",
        "GotoRoom",
        "Say",
        "UploadReq",
        "RecvFile",
        "ListFiles",
        "SendFile",
    ]

    def __init__(self, po: PostingEnd, ho: HostingEnd):
        self.po = po
        self.ho = ho

        self.in_room = prepare_room()
        self.nick = f"Stranger${self.po.remote_addr!s}"

    async def welcome_chatter(self):
        async with self.po.co() as co:
            # send welcome notice to new comer
            welcome_lines = [
                f"""
@@ Welcome {self.nick!s}, this is chat service at {self.ho.local_addr!s} !
 -
@@ There're {len(rooms)} room(s) open, and you are in #{self.in_room.room_id!s} now.
"""
            ]
            for room_id, room in rooms.items():
                welcome_lines.append(
                    f"""  -*-\t{len(room.chatters)!r} chatter(s) in room #{room.room_id!s}"""
                )
            welcome_text = "\n".join(str(line) for line in welcome_lines)
            await co.send_code(
                f"""
NickChanged({self.nick!r})
InRoom({self.in_room.room_id!r})
ShowNotice({welcome_text!r})
"""
            )

        # send new comer info to other chatters already in room
        async def notif_chatter_join(chatter: "Chatter"):
            assert chatter is not self, "how can that be ?!"
            await chatter.po.notif(
                f"""
ChatterJoined({self.nick!r}, {self.in_room.room_id!r})
"""
            )

        await self.in_room.each_in_room(notif_chatter_join)

        # add this chatter into its 1st room
        self.in_room.chatters.add(self)

    async def SetNick(self, nick: str):
        co: HoCo = self.ho.co()
        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        # note: the nick can be moderated here
        self.nick = str(nick).strip() or f"Anonymous@{self.po.remote_addr!s}"

        # peer expects the moderated new nick be sent back
        await co.send_obj(repr(self.nick))

    async def GotoRoom(self, room_id):
        co: HoCo = self.ho.co()
        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        old_room = self.in_room
        new_room = prepare_room(str(room_id).strip())

        # leave old room, enter new room
        old_room.chatters.discard(self)
        new_room.chatters.add(self)
        # change record state
        self.in_room = new_room

        welcome_lines = [
            f"""
@@ You are in #{new_room.room_id!s} now, {len(new_room.chatters)} chatter(s).
"""
        ]

        # send feedback
        room_msgs = new_room.recent_msg_log()
        welcome_text = "\n".join(str(line) for line in welcome_lines)
        await co.send_code(
            f"""
InRoom({new_room.room_id!r})
ShowNotice({welcome_text!r})
RoomMsgs({room_msgs!r})
"""
        )

        async def notif_chatter_leave(chatter: "Chatter"):
            if chatter is self:
                return  # may occur under frequent room changes like being spammed
            await chatter.po.notif(
                f"""
ChatterLeft({self.nick!r}, {old_room.room_id!r})
"""
            )

        async def notif_chatter_join(chatter: "Chatter"):
            if chatter is self:
                return  # may occur under frequent room changes like being spammed
            await chatter.po.notif(
                f"""
ChatterJoined({self.nick!r}, {new_room.room_id!r})
"""
            )

        # start new po co to others in new aio tasks to avoid deadlocks
        asyncio.create_task(old_room.each_in_room(notif_chatter_leave))
        asyncio.create_task(new_room.each_in_room(notif_chatter_join))

    # showcase a service method with binary payload, that to be received from
    # current hosting conversation
    async def Say(self, msg_id, msg_len: int):
        co: HoCo = self.ho.co()

        # receive & decode the input data
        assert isinstance(
            msg_len, int
        ), f"msg_len {msg_len!r} of type {type(msg_len)!r} instead of int ?!"
        msg_buf = bytearray(msg_len)
        await co.recv_data(msg_buf)
        msg = msg_buf.decode("utf-8")

        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        # post the msg to current room
        await self.in_room.post_msg(self, msg)

        # back-script the consumer to notify it about the success-of-display of the message
        await co.send_code(
            f"""
Said({msg_id!r})
"""
        )

    async def UploadReq(self, room_id: str, fn: str, fsz: int):
        co: HoCo = self.ho.co()
        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        if fsz > 200 * 1024 * 1024:  # 200 MB at most
            # send the reason as string, why it's refused
            await co.send_obj(repr(f"file too large!"))
            return
        if fsz < 2 * 1024:  # 2 KB at least
            # send the reason as string, why it's refused
            await co.send_obj(repr(f"file too small!"))
            return

        # None as refuse_reason means the upload is accepted
        await co.send_obj(None)

    async def RecvFile(self, room_id: str, fn: str, fsz: int):
        co: HoCo = self.ho.co()

        # TODO in a real world application, the same validation rules as in UploadReq()
        # should be checked again, or it's a security hole that a consumer can exploit.

        room_dir = os.path.abspath(f"chat-server-files/{room_id}")
        os.makedirs(room_dir, exist_ok=True)

        fpth = os.path.join(room_dir, fn)
        try:
            # unlink the file before creating a new one, so if someone has opened it for
            # download, that can finish normally.
            os.unlink(fpth)
        except FileNotFoundError:
            pass
        f = open(fpth, "wb")

        # prepare to recv file data from beginning, calculate chksum by the way
        chksum = 0

        try:

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

                    # time.sleep(0.01)  # simulate slow uploading

                assert bytes_remain == 0, "?!"

            # receive data stream from client
            await co.recv_data(stream_file_data())

        finally:
            f.close()

        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        # send back chksum for client to verify
        await co.send_obj(repr(chksum))

        # close the hosting conversation a.s.a.p.
        await co.close()

        # announce this new upload
        await self.in_room.post_msg(
            self,
            rf"""
 @*@ I just uploaded a file {chksum:x} {int(math.ceil(fsz / 1024))} KB [{fn}]
""",
        )

    async def ListFiles(self, room_id: str):
        co: HoCo = self.ho.co()
        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        room_dir = os.path.abspath(f"chat-server-files/{room_id}")
        if not os.path.isdir(room_dir):
            logger.info(f"Making room dir [{room_dir}] ...")
            os.makedirs(room_dir, exist_ok=True)

        fil = []
        for fn in os.listdir(room_dir):
            if fn[0] in ".~!?*":
                continue  # ignore strange file names
            try:
                s = os.stat(os.path.join(room_dir, fn))
            except OSError:
                pass
            fil.append([s.st_size, fn])

        # send back repr for peer to land & receive as obj
        await co.send_obj(repr(fil))

    async def SendFile(self, room_id: str, fn: str):
        co: HoCo = self.ho.co()
        # transit the hosting conversation to `send` stage a.s.a.p.
        await co.start_send()

        fpth = os.path.abspath(os.path.join("chat-server-files", room_id, fn))
        if not os.path.exists(fpth) or not os.path.isfile(fpth):
            # send negative file size, meaning download refused
            await co.send_obj(repr([-1, f"no such file"]))
            return

        s = os.stat(fpth)

        with open(fpth, "rb") as f:
            # get file data size
            f.seek(0, 2)
            fsz = f.tell()

            # send [file-size, msg] to peer, telling it the data size to receive and last
            # modification time of the file.
            msg = "last modified: " + datetime.fromtimestamp(s.st_mtime).strftime(
                "%F %T"
            )
            await co.send_obj(repr([fsz, msg]))

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

                    yield chunk  # yield it so as to be streamed to client
                    chksum = crc32(chunk, chksum)  # update chksum

                assert bytes_remain == 0, "?!"

            # stream file data to consumer end
            await co.send_data(stream_file_data())

        # send chksum at last
        await co.send_obj(repr(chksum))
