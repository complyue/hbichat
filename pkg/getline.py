import asyncio
import readline
import sys
import threading

from .log import get_logger

__all__ = ["GetLine"]

logger = get_logger(__name__)


class GetLine:
    """
    Async line getter.

    """

    def __init__(self, ps1):
        assert sys.stdin.isatty(), "should only use GetLine with terminal input!"

        self.ps1 = ps1

        self.loop = asyncio.get_running_loop()
        self.srcq = asyncio.Queue()

        self.procede_reading = threading.Event()
        self.prompting = False

    def read_loop(self):
        """
        This should be called from main thread to correctly receive KeyboardInterrupt (i.e. Ctrl^C)

        """

        while True:
            self.prompting = False
            self.procede_reading.wait()

            try:
                self.prompting = True
                s = input(self.ps1)
            except EOFError:
                # user pressed Ctrl^D to end reading
                # put cursor to next line
                print()
                # send None to source queue
                self.loop.call_soon_threadsafe(self.srcq.put_nowait, None)
                # stop the loop
                break
            except KeyboardInterrupt:
                # cancel current line, read again from scratch
                print("\r\x1B[0K", end="", flush=True)  # reset current line
                continue

            # send read source text to async queue
            self.loop.call_soon_threadsafe(self.srcq.put_nowait, s)

            # don't read input until next call on `.get_line()`
            self.procede_reading.clear()

    async def get_line(self):
        self.procede_reading.set()
        return await self.srcq.get()

    def show(self, text: str):
        """
        Print text to stdout without affecting pending line reading prompt and edit buffer.

        A new line is always added to the text.

        """
        assert isinstance(text, str), "only str should be passed here!"

        if len(text) <= 0:
            return

        # note the new line has to be combined with text into a single string.
        # if the new line is sent to `print()` as a separate arg, or as the `end=`
        # kwarg, the coming prompt (by an immediate subsequent `get_line()`) may
        # race to print into the middle.

        if not self.prompting:
            # just print the text
            print(text + "\n", end="")
            return

        # reset prompting line
        print("\r\x1B[0K", end="", flush=True)

        # print the text
        print(text + "\n", end="")

        # restore readline prompt and line buffer at new line
        if hasattr(readline, "rl_forced_update_display"):
            # not the case until issue 23067 finds its way into python release:
            #   https://bugs.python.org/issue23067
            readline.rl_forced_update_display()
        else:
            # this is rough, only correct when cursor not moved to middle of line buffer
            lb = readline.get_line_buffer()
            print(self.ps1, lb, sep="", end="", flush=True)
