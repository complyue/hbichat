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
        # this is essential for overall throughput with the underlying HBI wire.
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

    async def keep_chatting(self):

        hbic = self.po.hbic
        while hbic.is_connected():  # until disconnected from chat service

            sl = await self.line_getter.get_line()
            if sl is None:
                # User pressed Ctrl^D to end chatting.
                await hbic.disconnect()
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
            else:
                msg = sl
                await self._say(msg)

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
