import json
from pathlib import Path

import pytest

import stagehand
from stagehand import _runtime as runtime


def test_python_runtime_matches_shared_config_parity_fixture() -> None:
    fixture = _load_runtime_config_fixture()

    assert runtime.DEFAULT_CONFIG_FILENAME == fixture["default_config_filename"]
    assert list(runtime.VALID_MODES) == fixture["supported_modes"]
    assert runtime.ENV_SESSION == fixture["shared_env_keys"]["session"]
    assert runtime.ENV_MODE == fixture["shared_env_keys"]["mode"]
    assert runtime.ENV_CONFIG_PATH == fixture["shared_env_keys"]["config_path"]
    assert runtime.ENV_REPLAY_INPUT == fixture["shared_env_keys"]["replay_input"]
    assert stagehand.ENV_OPENAI_HOSTS == fixture["shared_env_keys"]["openai_hosts"]


def test_init_from_env_requires_session_and_mode(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.chdir(tmp_path)

    with pytest.raises(ValueError, match="STAGEHAND_SESSION and STAGEHAND_MODE must be set"):
        stagehand.init_from_env()

    monkeypatch.setenv("STAGEHAND_SESSION", "demo")
    with pytest.raises(ValueError, match="STAGEHAND_SESSION and STAGEHAND_MODE must be set"):
        stagehand.init_from_env()

    monkeypatch.delenv("STAGEHAND_SESSION", raising=False)
    monkeypatch.setenv("STAGEHAND_MODE", "record")
    with pytest.raises(ValueError, match="STAGEHAND_SESSION and STAGEHAND_MODE must be set"):
        stagehand.init_from_env()


def _load_runtime_config_fixture() -> dict[str, object]:
    fixture_path = (
        Path(__file__).resolve().parents[3]
        / "testdata"
        / "sdk-config-parity"
        / "runtime-bootstrap.json"
    )
    return json.loads(fixture_path.read_text(encoding="utf-8"))
