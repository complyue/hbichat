"""
Data structures to be passed over wire.

"""

__all__ = ["MsgsInRoom", "Msg"]


class MsgsInRoom:
    def __init__(self, room, msgs):
        self.room = room
        self.msgs = msgs

    def __repr__(self):
        return f"MsgsInRoom(({self.room!r}),({self.msgs!r}))"


class Msg:
    def __init__(self, from_, content, time_):
        self.from_ = from_
        self.content = content
        self.time_ = time_

    def __repr__(self):
        return f"Msg(({self.from_!r}),({content!r}),({time_!r}))"
