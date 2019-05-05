import asyncio
import threading

import hbi
from hbi import interop

from ...pkg import *
from ...pkg import ds

logger = get_logger(__package__)

# must be created in the thread running the asyncio loop where its
# `get_line()` will be awaited
line_getter = None
getting_line = threading.Event()  # get set after line_getter assigned


serving_addr = {"host": "127.0.0.1", "port": 3232}


po2peer: hbi.PostingEnd = None
ho4peer: hbi.HostingEnd = None

# get called when an hbi connection is made
def __hbi_init__(po, ho):
    global po2peer, ho4peer

    po2peer, ho4peer = po, ho


async def p3t(x):
    if not x:
        return
    line_getter.show("1:" + x)
    await asyncio.sleep(3)
    line_getter.show("2:" + x)
    await asyncio.sleep(3)
    line_getter.show("3:" + x)


async def do_chatting():
    global line_getter

    line_getter = GetLine("chat> ")
    getting_line.set()

    while True:
        sl = await line_getter.get_line()
        if sl is None:
            return
        asyncio.create_task(p3t(sl))

    hbic = hbi.HBIC(service_addr, globals())  # define the service connection
    # connect the service, get the posting endpoint
    async with hbic as po, ho:  # auto close hbic as a context manager
        await po.notif(
            f"""
SetNick('xxx')
"""
        )

        if sys.stdout.isatty():
            # after all text, spit <Esc>[0K to clear terminal current line up to EOL,
            # and use end='\r' to place the cursor back at line beginning for next print
            print(
                f"   {offset*100.0/tail_d:0.3g}% ({hrdsz(offset)}) emplaced,"
                f" pumping MSB #{msb_i+1:d} ({hrdsz(dsz)}) {inp}\x1B[0K",
                end="\r",
                flush=True,
            )


threading.Thread(target=asyncio.run, args=(do_chatting(),), name="ChatClient").start()

getting_line.wait()
assert line_getter is not None, "evt set without lg created ?!"
# read line forever in main thread
line_getter.read_loop()
