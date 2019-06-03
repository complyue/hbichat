import asyncio, hbi


async def say_hello_to(addr):
    po, ho = await hbi.dial_tcp(addr, hbi.HostingEnv())
    async with po.co() as co:
        await co.send_code(
            f"""
my_name = 'Nick'
hello()
"""
        )
        await co.start_recv()
        msg_back = await co.recv_obj()
    print(msg_back)
    await ho.disconnect()


asyncio.run(say_hello_to({"host": "127.0.0.1", "port": 3232}))
