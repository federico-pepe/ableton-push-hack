from __future__ import absolute_import

from .PushHackBrowser import PushHackBrowser


def create_instance(c_instance):
    return PushHackBrowser(c_instance)
