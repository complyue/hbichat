"""
simple chatting service implementation to show case HBI paradigms.

"""

import asyncio
import time
from collections import deque

import hbi

from .ds import *
from .log import *

__all__ = ["Room", "Chatter"]

logger = get_logger(__package__)


class Room:
    """
    Service side room object

    """

    def __init__(self, room_id: str, max_hist=10):
        self.room_id = room_id
        self.msgs = deque((), max_hist)
        self.cached_msg_log = None
        self.chatters = set()

    def recent_msg_log(self):
        if self.cached_msg_log is None:
            self.cached_msg_log = [*self.msgs]
        return self.cached_msg_log

    async def post_msg(self, from_chatter, content: str):
        msg = Msg(
            from_chatter.nick
            if isinstance(from_chatter, Chatter)
            else str(from_chatter),
            content,
            time.time(),
        )
        self.msgs.append(msg)
        self.cached_msg_log = None

        # notify all chatters but the OP in this room about the new msg
        room_msgs = MsgsInRoom(self.room_id, [msg])
        notif_code = rf"""
RoomMsgs({room_msgs!r})
"""
        loop = asyncio.get_running_loop()
        stale_chatters = set()
        for chatter in self.chatters:
            if not chatter.po.is_connected():
                stale_chatters.add(chatter)
                continue
            if chatter is from_chatter:  # no need to notify the OP about content
                continue
            try:
                # spawn the notification coroutine so the posting chatter's hosting conversation (which is
                # calling this coro) does not wait for end of this chatter's current conversation to finish
                # sending out of the `notif`.
                loop.create_task(chatter.po.notif(notif_code))
                # if the `notif()` shall be awaited here, care should be taken to avoid possible deadlocks.
            except Exception:
                logger.warning(
                    f"Dropping chat client [{chatter.po.net_ident}] due to error sending to it.",
                    exc_info=True,
                )
                stale_chatters.add(chatter)
        self.chatters -= stale_chatters


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

    # name of methods to be exposed for peer scripting
    service_methods = ["__hbi_init__", "SetNick", "GotoRoom", "Say", "hbi_disconnected"]

    def __init__(self, po: hbi.PostingEnd, ho: hbi.HostingEnd):
        self.po = po
        self.ho = ho

        self.in_room = prepare_room()
        self.nick = f"Stranger@{self.po.remote_addr!s}"

    async def __hbi_init__(po: hbi.PostingEnd, ho: hbi.HostingEnd):
        assert po is self.po and ho is self.ho

        async with po.co() as co:
            # send welcome notice to new comer
            welcome_lines = [
                f"""
Welcome to chat service at {ho.local_addr!s} !
  ---
There're {len(rooms)} rooms open, while you are in #{self.in_room.room_id!s} now.
"""
            ]
            for room in rooms:
                welcome_lines.append(
                    f"""  -*-\t{len(room.chatters)!r} chatter(s) in room #{room.room_id!s}"""
                )
            await co.send_code(
                f"""
ShowNotice({",".join(repr(line) for line in welcome_lines)})
InRoom({self.in_room.room_id!r})
"""
            )

            # send new comer info to other chatters already in room
            for chatter in self.in_room.chatters:
                await chatter.po.notif(
                    f"""
ChatterJoined({self.nick!r}, {self.in_room.room_id!r})
"""
                )

        self.in_room.chatters.add(self)

    # show case a simple synchronous service method
    def SetNick(self, nick):
        self.nick = nick or f"Anonymous@{self.po.remote_addr!s}"

    # show case a simple asynchronous service method
    async def GotoRoom(self, room_id):
        old_room = self.in_room
        new_room = prepare_room(room_id)

        old_room.chatters.remove(self)
        for chatter in old_room.chatters:
            await chatter.po.notif(
                f"""
ChatterLeft({self.nick!r}, {old_room.room_id!r})
"""
            )
        for chatter in new_room.chatters:
            await chatter.po.notif(
                f"""
ChatterJoined({self.nick!r}, {new_room.room_id!r})
"""
            )

        self.in_room = new_room
        new_room.chatters.add(self)
        welcome_lines = [
            f"""
You are in #{new_room.room_id!s} now, {len(new_room.chatters)} chatter(s).
"""
        ]
        room_msgs = MsgsInRoom(new_room.room_id, new_room.recent_msg_log)
        async with self.po.co() as co:
            await co.send_code(
                f"""
ShowNotice({",".join(repr(line) for line in welcome_lines)})
InRoom({new_room.room_id!r})
RoomMsgs({room_msgs!r})
"""
            )

    # show case a service method with binary payload, that to be received from
    # current hosting conversation
    async def Say(self, msg_id, msg_len):

        # decode the input data
        msg_buf = bytearray(msg_len)
        await self.ho.co.recv_data(msg_buf)
        msg = msg_buf.decode("utf-8")

        # use the input data
        await self.in_room.post_msg(self, msg)

        # asynchronously feedback result of the method call
        await self.ho.co.send_code(
            f"""
Said({msg_id!r})
"""
        )

    # show case the hbi callback on wire disconnected
    def hbi_disconnected(self, exc=None):
        self.in_room.chatters.remove(self)
