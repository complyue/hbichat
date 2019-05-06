"""
simple chatting service implementation to show case HBI paradigms.

name of this package starts with an under score so that it's hidden 
from exports of the root `hbichat` package. while the `consumer` 
package is supposed to be part of the public exports.

"""
from .chatter import *
from .room import *

__all__ = [

    # exports from .chatter
    'Chatter',

    # exports from .room
    'Room',

]
