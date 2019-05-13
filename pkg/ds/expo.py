from hbi import HostingEnv

from .mir import *

__all__ = ["expose_shared_data_structures"]


def expose_shared_data_structures(he: HostingEnv):

    he.expose_ctor(MsgsInRoom)
    he.expose_ctor(Msg)
