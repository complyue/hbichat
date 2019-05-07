import argparse
import asyncio
import sys
import threading

import hbi

from ...pkg import *
from ...pkg import ds

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

# define global var for the chatter consumer instance
# the instance must be created in the thread running the asyncio loop,
# where its `line_getter.get_line()` coroutine will be awaited.
chatter = None
getting_line = threading.Event()  # get set after chatter created & assigned


async def do_chatting():
    global chatter

    try:

        ps1 = f">{service_addr['host']!s}:{service_addr['port']!s}> "
        chatter = Chatter(GetLine(ps1))
        getting_line.set()

        # define the service connection
        hbic = hbi.HBIC(
            # the service address
            service_addr,
            # the consumer context
            {
                # expose standard named values for interop
                **{x: getattr(hbi.interop, x) for x in hbi.interop.__all__},
                # expose all shared type of data structures
                **{x: getattr(ds, x) for x in ds.__all__},
                # expose all consumer methods of the chatter instance
                **{mth: getattr(chatter, mth) for mth in chatter.consumer_methods},
            },
        )
        # connect the service, get the posting & hosting endpoint
        async with hbic as (po, ho):  # auto close hbic as a context manager

            # keep chatting until user exit or connection lost etc.
            await chatter.keep_chatting()

        logger.debug("Done chatting.")

    except Exception:
        import os, signal

        logger.fatal(f"Error in chatting.", exc_info=True)

        # `threading.Event.wait` won't catch SystemExit so far.
        # by sending self a SIGINT, we then make KeyboardInterrupt caught in
        # `line_getter.read_loop()`
        os.kill(os.getpid(), signal.SIGINT)

        sys.exit(3)


# run coroutines in another dedicated thread, so as to spare main thread to run TUI loop
threading.Thread(target=asyncio.run, args=(do_chatting(),), name="ChatClient").start()

getting_line.wait()  # wait until the chatter consumer instance is created
assert chatter is not None
# read line forever in main thread, this is necessary for `KeyboardInterrupt`
# to be properly caught by the TUI loop.
chatter.line_getter.read_loop()
