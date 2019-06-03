import asyncio, hbi


def he_factory() -> hbi.HostingEnv:
    he = hbi.HostingEnv()

    he.expose_function(
        "__hbi_init__",  # callback on wire connected
        lambda po, ho: po.notif(
            f"""
print("Welcome to HBI world!")
"""
        ),
    )

    async def hello():
        co = he.ho.co()
        await co.start_send()
        consumer_name = he.get("my_name")
        await co.send_obj(repr(f"Hello, {consumer_name} from {he.po.remote_addr}!"))

    he.expose_function("hello", hello)

    return he


async def serve_hello():

    server = await hbi.serve_tcp(
        {"host": "127.0.0.1", "port": 3232},  # listen address
        he_factory,  # factory for hosting environment
    )
    print("hello server listening:", server.sockets[0].getsockname())
    await server.wait_closed()


try:
    asyncio.run(serve_hello())
except KeyboardInterrupt:
    pass
