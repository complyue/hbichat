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
    def __init__(self, name: str, max_hist=10):
        self.name = name
        self.msgs = deque((), max_hist)
        self.cached_msg_log = None
        self.chatters = set()

    def recent_msg_log(self):
        if self.cached_msg_log is None:
            self.cached_msg_log = [*self.msgs]
        return self.cached_msg_log

    async def post_msg(self, from_chatter, content: str):
        msg = Msg(from_chatter.nick, content, time.time())
        self.msgs.append(msg)
        self.cached_msg_log = None

        notif_code = rf"""
RoomMsgs({MsgsInRoom(self.name, [msg])!r})
"""
        stale_chatters = set()
        for chatter in self.chatters:
            if not chatter.po.is_connected():
                stale_chatters.add(chatter)
                continue
            try:
                if chatter is from_chatter:
                    # called from its current hosting conversation (which is `.ho.co`),
                    # use a new posting conversation will deadlock here, so use the ho/co
                    await chatter.ho.co.send_code(notif_code)
                else:
                    # `po.notif()` uses an implicit posting conversation
                    await chatter.po.notif(notif_code)
            except Exception:
                logger.warning(
                    f"Dropping chat client [{chatter.po.net_ident}] due to error sending it.",
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

    # name of methods to be exposed for peer scripting
    service_methods = [
        "__hbi_init__",
        "set_nick",
        "goto_room",
        "say",
        "hbi_disconnected",
    ]

    def __init__(self, po: hbi.PostingEnd, ho: hbi.HostingEnd):
        self.po = po
        self.ho = ho

        self.in_room = prepare_room()
        self.nick = f"Stranger@{po.remote_addr!s}"

    async def __hbi_init__(po: hbi.PostingEnd, ho: hbi.HostingEnd):
        assert po is self.po and ho is self.ho

        # TODO send welcome msg to new comer
        # TODO send member coming msg to room

    # show case a simple synchronous service method
    def set_nick(self, nick):
        self.nick = nick or f"Anonymous@{self.po.remote_addr!s}"

    # show case a simple asynchronous service method
    async def goto_room(self, room_id):
        old_room = self.in_room
        new_room = prepare_room(room_id)

        async with old_room.chatters_lock, new_room.chatters_lock:
            old_room.chatters.remove(self)
            new_room.chatters.add(self)

        self.in_room = new_room

        welcome_msg = rf"""
"""
        po.notif(
            rf"""
EnteredRoom({room.name!r},{welcome_msg!r})
"""
        )

    # show case a service method with binary payload, that to be received from
    # current hosting conversation
    async def say(self, msg_id, msg_len):

        # decode data
        msg_buf = bytearray(msg_len)
        await self.ho.co.recv_data(msg_buf)
        msg = msg_buf.decode("utf-8")

        # spread data
        ...

        # async feedback of result
        await self.ho.co.send_code(
            f"""
said({msg_id!r})
"""
        )

    # an hbi callback
    def hbi_disconnected(self, exc=None):
        self.in_room.chatters.remove(self)
