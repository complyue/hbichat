import asyncio
import time
from collections import deque

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
