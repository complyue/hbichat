import struct
import time
from datetime import datetime

__all__ = ["MsgsInRoom", "Msg"]


class MsgsInRoom:
    def __init__(self, room_id, msgs):
        self.room_id = room_id
        self.msgs = msgs

    def __repr__(self):
        return f"MsgsInRoom(({self.room_id!r}),({self.msgs!r}))"


class Msg:
    def __init__(self, from_, content, time_):
        self.from_ = str(from_)
        self.content = str(content)
        if time_ is None:
            self.time_ = time.time()
        elif isinstance(time_, float):
            self.time_ = time_
        elif isinstance(time_, datetime):
            self.time_ = time_.timestamp()
        else:
            raise ValueError(f"Invalid time type: {type(time_)!s}")

    def __repr__(self):
        return f"Msg(({self.from_!r}),({self.content!r}),({self.time_!r}))"

    def __str__(self):
        return f"[{datetime.fromtimestamp(self.time_).strftime('%F %T')!s}] {self.from_!s}: {self.content!s}"
