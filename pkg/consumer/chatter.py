import asyncio

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

    # name of methods to be exposed for peer scripting
    consumer_methods = [
        "__hbi_init__",
        "ShowNotice",
        "NickAccepted",
        "InRoom",
        "RoomMsgs",
        "Said",
        "ChatterJoined",
        "ChatterLeft",
        "hbi_disconnected",
    ]

    def __init__(self, line_getter: GetLine):
        self.line_getter = line_getter

        self.nick = "?"
        self.in_room = "?"
        self.sent_msgs = []

        self.po = None
        self.ho = None

    async def __hbi_init__(self, po: hbi.PostingEnd, ho: hbi.HostingEnd):
        self.po = po
        self.ho = ho

    def _update_prompt(self):
        self.line_getter.ps1 = f"{self.nick!s}@{self.po.remote_addr!s}#{self.in_room}: "

    def NickAccepted(self, nick: str):
        self.nick = nick
        self._update_prompt()

    def InRoom(self, room_id: str):
        self.in_room = room_id
        self._update_prompt()

    def RoomMsgs(self, room_msgs: MsgsInRoom):
        if room_msgs.room_id != self.in_room:
            self.line_getter.show(f" *** Messages from #{room_msgs.room_id!s} ***")
        self.line_getter.show("\n".join(str(msg) for msg in room_msgs.msgs))

    async def keep_chatting(self):

        while self.ho.is_connected():

            sl = await self.line_getter.get_line()
            if sl is None:
                # User pressed Ctrl^D to end chatting.
                break
            sl = sl.strip()

            if len(sl) < 1:  # only white space(s) or just enter pressed
                continue

            if sl[0] == "#":
                # goto the specified room
                room_id = sl[1:].strip()
                await self.po.notif(
                    rf"""
GotoRoom({room_id!r})
"""
                )
            elif sl[0] == "$":
                # change nick
                nick = sl[1:].strip()
                await self.po.notif(
                    rf"""
SetNick({nick!r})
"""
                )
            else:
                msg = sl
                try:
                    # try find an empty slot to hold this pending message
                    msg_id = self.sent_msgs.index(None)
                    self.sent_msgs[msg_id] = msg
                except ValueError:
                    # extend a new slot for this pending message
                    msg_id = len(self.sent_msgs)
                    self.sent_msgs.append(msg)
                msg_buf = msg.encode("utf-8")
                await self.po.notif_data(
                    rf"""
Say({msg_id!r}, {len(msg_buf)!r})
""",
                    msg_buf,
                )

        print("Bye.")

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

    # show case the hbi callback on wire disconnected
    def hbi_disconnected(self, exc=None):
        if exc is not None:
            logger.error(f"Connection to chatting service lost: {exc!s}")
        self.line_getter.stop()
