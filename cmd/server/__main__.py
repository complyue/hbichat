import asyncio

import hbi

from ..pkg import *

logger = get_logger(__package__)


serving_addr = {"host": "127.0.0.1", "port": 3232}


# create an isolated context for each consumer connection
def create_chatter_serving_ctx(po, ho) -> dict:
    chatter = Chatter(po, ho)
    return {mth: getattr(chatter, mth) for mth in chatter.service_methods}


async def serve_jobs():
    await hbi.HBIS(
        # listening IP address
        serving_addr,
        # the service context factory function,
        create_chatter_serving_ctx,
    ).serve_until_closed()


asyncio.run(serve_jobs())
