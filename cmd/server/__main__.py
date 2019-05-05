import asyncio

import hbi

from hbi import interop

from ...pkg import *
from ...pkg import ds

logger = get_logger(__package__)


serving_addr = {"host": "127.0.0.1", "port": 3232}


# define the service context factory function to:
# create an isolated context for each consumer connection
def create_chatter_serving_ctx(po, ho) -> dict:

    # create a serving instance for this service consumer connection
    chatter = Chatter(po, ho)

    return {
        # expose standard named values for interop
        **{x: getattr(interop, x) for x in interop.__all__},
        # expose all shared type of data structures
        **{x: getattr(ds, x) for x in ds.__all__},
        # expose all service methods of the chatter instance
        **{mth: getattr(chatter, mth) for mth in chatter.service_methods},
    }


async def serve_chatting():
    await hbi.HBIS(
        # listening IP address
        serving_addr,
        # the service context factory function
        create_chatter_serving_ctx,
    ).serve_until_closed()


asyncio.run(serve_chatting())
