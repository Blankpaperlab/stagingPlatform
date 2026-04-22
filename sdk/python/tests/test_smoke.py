from pathlib import Path

import pytest

import stagehand


def test_version_is_defined() -> None:
    assert stagehand.__version__ == "0.1.0a0"


def test_init_returns_runtime_metadata(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(tmp_path)

    runtime = stagehand.init(session="demo", mode="record")

    assert runtime.session == "demo"
    assert runtime.mode == "record"
    assert runtime.run_id.startswith("run_")
    assert runtime.config_path is None
    assert stagehand.is_initialized() is True
    assert stagehand.get_runtime() == runtime
    assert runtime.recorder_metadata()["session"] == "demo"


def test_init_auto_discovers_stagehand_config(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    config_path = tmp_path / "stagehand.yml"
    config_path.write_text("schema_version: v1alpha1\n", encoding="utf-8")
    monkeypatch.chdir(tmp_path)

    runtime = stagehand.init(session="demo", mode="replay")

    assert runtime.mode == "replay"
    assert runtime.config_path == str(config_path.resolve())


def test_init_accepts_explicit_config_path(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(tmp_path)
    config_path = tmp_path / "custom.stagehand.yml"
    config_path.write_text("schema_version: v1alpha1\n", encoding="utf-8")

    runtime = stagehand.init(session="demo", mode="passthrough", config_path=config_path)

    assert runtime.mode == "passthrough"
    assert runtime.config_path == str(config_path.resolve())


def test_double_init_is_rejected(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(tmp_path)
    stagehand.init(session="demo", mode="record")

    with pytest.raises(stagehand.AlreadyInitializedError):
        stagehand.init(session="demo-again", mode="record")


def test_invalid_mode_is_rejected(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(tmp_path)

    with pytest.raises(stagehand.InvalidModeError):
        stagehand.init(session="demo", mode="hybrid")


def test_empty_session_is_rejected(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(tmp_path)

    with pytest.raises(ValueError):
        stagehand.init(session="   ", mode="record")


def test_missing_config_path_is_rejected(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.chdir(tmp_path)

    with pytest.raises(FileNotFoundError):
        stagehand.init(session="demo", mode="record", config_path=tmp_path / "missing.yml")


def test_get_runtime_requires_init() -> None:
    with pytest.raises(stagehand.NotInitializedError):
        stagehand.get_runtime()
