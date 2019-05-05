import asyncio

import hbi
from hbi import interop

from ..pkg import *
from ..pkg import ds

logger = get_logger(__package__)


serving_addr = {"host": "127.0.0.1", "port": 3232}


po2peer: hbi.PostingEnd = None
ho4peer: hbi.HostingEnd = None

# get called when an hbi connection is made
def __hbi_init__(po, ho):
    global po2peer, ho4peer

    po2peer, ho4peer = po, ho


async def do_chatting():
    hbic = hbi.HBIC(service_addr, globals())  # define the service connection
    # connect the service, get the posting endpoint
    async with hbic as po, ho:  # auto close hbic as a context manager
        await po.notif(
            f"""
SetNick('xxx')
"""
        )


asyncio.run(do_chatting)
