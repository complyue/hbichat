import asyncio
import time
from collections import deque
from typing import *

import hbi

from ..ds import *
from ..log import *

__all__ = ["Room"]

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

    async def each_in_room(self, with_chatter: Callable[["Chatter"], None]):
        # enumerate chatters in room, drop those disconnected and causing errors
        err_chatters = set()
        for chatter in [  # snapshot the chatters set into a list for enumeration
            *self.chatters
        ]:
            try:
                await with_chatter(chatter)
            except Exception:
                if not chatter.po.is_connected():
                    err_chatters.add(chatter)
        if err_chatters:
            self.chatters -= err_chatters

    def recent_msg_log(self):
        if self.cached_msg_log is None:
            self.cached_msg_log = MsgsInRoom(self.room_id, [*self.msgs])
        return self.cached_msg_log

    async def post_msg(self, from_chatter, content: str):
        from .chatter import Chatter

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

        async def deliver_room_msg(chatter: "Chatter"):
            if chatter is from_chatter:
                return  # not to the OP
            await chatter.po.notif(notif_code)

        # send notification to others in a separated aio task to avoid deadlock
        asyncio.create_task(self.each_in_room(deliver_room_msg))
