import argparse
import asyncio
import runpy

from hbi import *

from ...pkg._service import *
from ...pkg.ds import *
from ...pkg.log import *

logger = get_logger(__package__)


# take arguments from command line
cmdl_parser = argparse.ArgumentParser(
    prog="python -m hbichat.cmd.server",
    description="HBI chatting server",
    epilog="start a chat server to host chatting",
)
cmdl_parser.add_argument(
    "addr",
    metavar="service_address",
    nargs="?",
    const="127.0.0.1:3232",
    help="in form of <ip>:<port>",
)
cmdl_parser.add_argument(
    "-i",
    "--ip",
    metavar="listen_ip",
    nargs="+",
    default=None,
    help="host IP address(es) to listen on",
)
cmdl_parser.add_argument(
    "-p",
    "--port",
    metavar="listen_port",
    nargs=1,
    default=None,
    help="IP port number to listen on",
)
prog_args = cmdl_parser.parse_args()

# apply command line arguments
service_addr = {"host": [], "port": 3232}
if prog_args.addr is not None:
    host, *port = prog_args.addr.rsplit(":", 1)
    service_addr["host"] = [host]
    if port:
        service_addr["port"] = int(port[0])
if prog_args.ip is not None:
    for ip in prog_args.ip:
        service_addr["host"].append(ip)
if prog_args.port is not None:
    service_addr["port"] = prog_args.port[0]
if len(service_addr["host"]) < 1:
    # listen on loopback by default
    service_addr["host"].append("127.0.0.1")


def he_factory():  # Create a hosting env reacting to chat consumers
    he = HostingEnv()

    chatter = None  # the main reactor object

    async def __hbi_init__(po: PostingEnd, ho: HostingEnd):
        nonlocal chatter

        # create a chatter service instance and expose as reactor
        chatter = Chatter(po, ho)
        he.expose_reactor(chatter)

        # send welcome message to new comer
        await chatter.welcome_chatter()

    async def __hbi_cleanup__(po: PostingEnd, ho: HostingEnd, err_reason=None):
        nonlocal chatter

        if err_reason is not None:
            logger.error(
                f"Connection to chatting consumer {chatter.po.remote_addr!s} lost: {err_reason!s}"
            )
        else:
            logger.debug(f"Chatting consumer {chatter.po.remote_addr!s} disconnected.")

        chatter.in_room.chatters.discard(chatter)

    # expose standard named values for interop
    expose_interop_values(he)

    # expose all shared type of data structures
    expose_shared_data_structures(he)

    # expose magic functions
    he.expose_function(None, __hbi_init__)
    he.expose_function(None, __hbi_cleanup__)

    return he


async def serve_chatting():
    server = await serve_socket(
        # listening IP address(es)
        service_addr,
        # the hosting env factory function
        he_factory,
    )
    logger.info(
        "HBI Chatting Server listening:\n  * "
        + "\n  * ".join(
            ":".join(str(v) for v in s.getsockname()) for s in server.sockets
        )
    )

    await server.wait_closed()


handle_signals()

try:
    asyncio.run(serve_chatting())
except KeyboardInterrupt:
    logger.info("HBI Chatting Server shut down.")
