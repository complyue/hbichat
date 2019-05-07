if "__chat_server__" == __name__:
    # Initializing HBI context reacting to chat consumers.

    # expose standard named values for interop
    from hbi.interop import *

    # expose all shared type of data structures
    from ...pkg.ds import *

    async def __hbi_init__(po, ho):
        from ...pkg._service import Chatter

        # expose chatter instance
        global chatter

        # the chatter service instance
        chatter = Chatter(po, ho)

        # expose all service methods of the chatter instance
        globals().update(
            {mth: getattr(chatter, mth) for mth in chatter.service_methods}
        )

        # send welcome message to new comer
        await chatter.welcome_chatter()

    # show case the hbi callback on wire disconnected
    def hbi_disconnected(self, exc=None):
        if exc is not None:
            logger.error(
                f"Connection to chatting consumer {chatter.po.remote_addr!s} lost: {exc!s}"
            )

        chatter.in_room.chatters.remove(chatter)


elif "__main__" == __name__:
    # entry point of `python -m hbichat.cmd.server`

    import argparse
    import asyncio
    import runpy

    import hbi

    from ...pkg._service import *
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

    async def serve_chatting():
        server = await hbi.HBIS(
            # listening IP address(es)
            service_addr,
            # the service context factory function
            lambda po, ho: runpy.run_module(
                # reuse this module file for both service context and `python -m` entry point
                mod_name=__package__,
                # invoke HBI context part of this module
                run_name="__chat_server__",
            ),
        ).server()
        logger.info(
            "HBI Chatting Server listening:\n  * "
            + "\n  * ".join(
                ":".join(str(v) for v in s.getsockname()) for s in server.sockets
            )
        )

        try:
            await server.wait_closed()
        except KeyboardInterrupt:
            logger.info("HBI Chatting Server shutting down.")
            server.close()
            await server.wait_closed()

    try:
        asyncio.run(serve_chatting())
    except KeyboardInterrupt:
        logger.info("HBI Chatting Server shut down.")
