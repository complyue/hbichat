import asyncio
import sys
import threading

from .log import get_logger

__all__ = ["AsyncRPL"]

logger = get_logger(__name__)


class AsyncRPL:
    """
    Read-Print-Loop in a dedicated thread to interface with asyncio coroutines for REPL impl.

    """

    def __init__(self, ps1):
        self.ps1 = ps1

        self.procede_reading = threading.Event()

        self.loop = asyncio.get_running_loop()
        self.srcq = asyncio.Queue()

        self._th = threading.Thread(target=self._read_loop)
        self._th.start()

    def _read_loop(self):
        while True:
            self.procede_reading.wait()

            s = input(self.ps1)

            self.procede_reading.clear()

            loop.call_soon_threadsafe(self.srcq.put_nowait, s)

    async def read_source(self, out_text=None, end="\n"):
        if out_text is not None:
            print(out_text, flush=True, end=end)
        self.procede_reading.set()
        return await self.srcq.get()
