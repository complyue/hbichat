import asyncio
import time
from collections import deque

import hbi

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

    # name of methods to be exposed for peer scripting
    service_methods = ["__hbi_init__", "SetNick", "GotoRoom", "Say", "hbi_disconnected"]

    def __init__(self, po: hbi.PostingEnd, ho: hbi.HostingEnd):
        self.po = po
        self.ho = ho

        self.in_room = prepare_room()
        self.nick = f"Stranger?"

    async def __hbi_init__(self, po: hbi.PostingEnd, ho: hbi.HostingEnd):
        assert po is self.po and ho is self.ho

        self.nick = f"Stranger${self.po.remote_addr!s}"

        async with po.co() as co:
            # send welcome notice to new comer
            welcome_lines = [
                f"""
@@ Welcome {self.nick!s}, this is chat service at {ho.local_addr!s} !
 -
@@ There're {len(rooms)} rooms open, and you are in #{self.in_room.room_id!s} now.
"""
            ]
            for room_id, room in rooms.items():
                welcome_lines.append(
                    f"""  -*-\t{len(room.chatters)!r} chatter(s) in room #{room.room_id!s}"""
                )
            notice_text = "\n".join(str(line) for line in welcome_lines)
            await co.send_code(
                f"""
NickAccepted({self.nick!r})
InRoom({self.in_room.room_id!r})
ShowNotice({notice_text!r})
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

    async def SetNick(self, nick: str):
        self.nick = str(nick).strip() or f"Anonymous@{self.po.remote_addr!s}"
        await self.ho.co.send_code(
            rf"""
NickAccepted({self.nick!r})
ShowNotice({"You are now known as `"+self.nick+"`"!r})
"""
        )

    # show case a simple asynchronous service method
    async def GotoRoom(self, room_id):
        old_room = self.in_room
        new_room = prepare_room(str(room_id).strip())

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
@@ You are in #{new_room.room_id!s} now, {len(new_room.chatters)} chatter(s).
"""
        ]
        room_msgs = MsgsInRoom(new_room.room_id, new_room.recent_msg_log())
        notice_text = "\n".join(str(line) for line in welcome_lines)
        await self.ho.co.send_code(
            f"""
InRoom({new_room.room_id!r})
ShowNotice({notice_text!r})
RoomMsgs({room_msgs!r})
"""
        )

    # show case a service method with binary payload, that to be received from
    # current hosting conversation
    async def Say(self, msg_id, msg_len: int):

        # decode the input data
        assert isinstance(
            msg_len, int
        ), f"msg_len {msg_len!r} of type {type(msg_len)!r} instead of int ?!"
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
