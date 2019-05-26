import argparse
import asyncio
import os
import runpy
import signal
import sys
import threading

from hbi import *

from ...pkg import *
from ...pkg.ds import *
from ...pkg.log import *

logger = get_logger(__package__)


if not sys.stdout.isatty():
    logger.fatal("Can only run with a terminal!")
    sys.exit(1)

# take arguments from command line
cmdl_parser = argparse.ArgumentParser(
    prog="python -m hbichat.cmd.client",
    description="HBI chatting client",
    epilog="connect to a chat server to start chatting",
)
cmdl_parser.add_argument(
    "addr",
    metavar="service_address",
    nargs="?",
    const="localhost:3232",
    help="in form of <host>:<port>",
)
cmdl_parser.add_argument(
    "-s",
    "--server",
    metavar="server_host",
    nargs=1,
    default=None,
    help="server IP or name",
)
cmdl_parser.add_argument(
    "-p", "--port", metavar="server_port", nargs=1, default=None, help="IP port number"
)
prog_args = cmdl_parser.parse_args()

# apply command line arguments
service_addr = {"host": None, "port": 3232}
if prog_args.addr is not None:
    host, *port = prog_args.addr.rsplit(":", 1)
    service_addr["host"] = host
    if port:
        service_addr["port"] = int(port[0])
if prog_args.server is not None:
    service_addr["host"] = prog_args.server[0]
if prog_args.port is not None:
    service_addr["port"] = prog_args.port[0]
if not service_addr["host"]:
    service_addr["host"] = "127.0.0.1"


# the line getter for simple terminal UI.
# it'll be set by a coroutine upon HBI connection made to chat service,
# the main thread waits until it's set, then runs its UI loop.
tui_liner = SyncVar()


def create_he():  # Create a hosting env reacting to chat service
    he = HostingEnv()

    async def __hbi_init__(po: PostingEnd, ho: HostingEnd):
        # sync variables to be set, they are read by main thread
        global tui_liner

        # TUI loop of this line getter is to be run by main thread
        line_getter = GetLine(f">{po.remote_addr!s}> ")

        # create a chatter consumer instance and expose as reactor
        chatter = Chatter(line_getter, po, ho)
        he.expose_reactor(chatter)

        # assign the sync variable to tell main thread to start TUI loop
        tui_liner.set(line_getter)

        asyncio.create_task(chatter.keep_chatting())

    async def __hbi_cleanup__(po: PostingEnd, ho: HostingEnd, err_reason=None):

        if err_reason is not None:
            logger.error(f"Error with chatting service: {err_reason!s}")

        if not tui_liner.is_set():
            # let main thread quit instead of wait for this forever
            tui_liner.set(None)

    # expose standard named values for interop
    expose_interop_values(he)

    # expose all shared type of data structures
    expose_shared_data_structures(he)

    # expose magic functions
    he.expose_function(None, __hbi_init__)
    he.expose_function(None, __hbi_cleanup__)

    return he


line_getter = None


async def do_chatting():
    global line_getter

    try:

        # connect the service, get the posting & hosting endpoint
        po, ho = await dial_socket(
            # the service address
            service_addr,
            # the consumer hosting env
            create_he(),
        )

        await ho.wait_disconnected()

        logger.debug("Done chatting.")

    except Exception:
        logger.fatal(f"Error in chatting.", exc_info=True)

    if not tui_liner.is_set():
        # let main thread quit instead of wait for this forever
        tui_liner.set(None)
    else:
        assert line_getter is tui_liner.val

        if line_getter.running:

            # send a SIGINT to self, make sure a KeyboardInterrupt get caught in `line_getter.read_loop()`
            os.kill(os.getpid(), signal.SIGINT)

            line_getter.stop()


handle_signals()

# run coroutines in another dedicated thread, so as to spare main thread to run TUI loop
threading.Thread(target=asyncio.run, args=[do_chatting()], name="ChatClient").start()

# obtain the line getter, it'll be set by a coroutine upon HBI connection made to chat service
line_getter = tui_liner.get()

# read line forever in main thread, this is necessary for `KeyboardInterrupt`
# to be properly caught by the TUI loop.
if line_getter is not None:
    line_getter.read_loop()
