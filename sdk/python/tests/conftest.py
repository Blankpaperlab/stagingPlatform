import pytest

from stagehand._runtime import _reset_for_tests


@pytest.fixture(autouse=True)
def reset_stagehand_runtime() -> None:
    _reset_for_tests()
    yield
    _reset_for_tests()
