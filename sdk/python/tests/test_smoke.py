import pytest

import stagehand


def test_version_is_defined() -> None:
    assert stagehand.__version__ == "0.1.0a0"


def test_init_is_not_implemented_yet() -> None:
    with pytest.raises(NotImplementedError):
        stagehand.init(session="demo", mode="record")
