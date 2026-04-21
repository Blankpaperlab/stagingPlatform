"""Stagehand Python SDK package."""

from ._version import ARTIFACT_VERSION, __version__


def init(*args, **kwargs):
    raise NotImplementedError("Stagehand Python SDK init will be implemented in Story D1.")


__all__ = ["ARTIFACT_VERSION", "__version__", "init"]
