if "__chat_client__" == __name__:
    # Initializing HBI context reacting to chat service.

    # expose standard named values for interop
    from hbi.interop import *

    # expose all shared type of data structures
    from ...pkg.ds import *

    async def __hbi_init__(po, ho):
        from ...pkg import GetLine, Chatter

        # sync variables to be set, they are read by main thread
        global tui_liner

        # expose chatter instance
        global chatter

        # TUI loop of this line getter is to be run by main thread
        line_getter = GetLine(f">{po.remote_addr!s}> ")

        # the chatter consumer instance
        chatter = Chatter(line_getter, po, ho)

        # expose all consumer methods of the chatter instance
        globals().update(
            {mth: getattr(chatter, mth) for mth in chatter.consumer_methods}
        )

        # assign the sync variable to tell main thread to start TUI loop
        tui_liner.set(line_getter)

    # show case the hbi callback on wire disconnected
    def hbi_disconnected(exc=None):
        if exc is not None:
            logger.error(f"Connection to chatting service lost: {exc!s}")

        # stop TUI loop in main thread
        if tui_liner.is_set():
            tui_liner.get().stop()


elif "__main__" == __name__:
    # entry point of `python -m hbichat.cmd.client`

    import argparse
    import asyncio
    import runpy
    import sys
    import threading

    import hbi

    from ...pkg import *

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
        "-p",
        "--port",
        metavar="server_port",
        nargs=1,
        default=None,
        help="IP port number",
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

    tui_liner = SyncVar()

    async def do_chatting():
        try:

            # define the service connection
            hbic = hbi.HBIC(
                # the service address
                service_addr,
                # the consumer context
                runpy.run_module(
                    # reuse this module file for both consumer context and `python -m` entry point
                    mod_name=__package__,
                    # invoke HBI context part of this module
                    run_name="__chat_client__",
                    # pass sync variables for coroutines to assign
                    init_globals={"tui_liner": tui_liner},
                ),
            )
            # connect the service, get the posting & hosting endpoint
            async with hbic as (po, ho):  # auto close hbic as a context manager

                # keep chatting until user exit or connection lost etc.
                await hbic.ctx["chatter"].keep_chatting()

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
    threading.Thread(
        target=asyncio.run, args=[do_chatting()], name="ChatClient"
    ).start()

    # obtain the line getter, it'll be set by a coroutine upon HBI connection made to chat service
    line_getter = tui_liner.get()

    # read line forever in main thread, this is necessary for `KeyboardInterrupt`
    # to be properly caught by the TUI loop.
    line_getter.read_loop()
