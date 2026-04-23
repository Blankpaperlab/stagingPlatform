from __future__ import annotations

import atexit
import json
import os
import sys
import tempfile
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from threading import Lock
from typing import Any, Final, Iterable, Literal, TypeAlias
from uuid import uuid4

from ._capture import CaptureBuffer, CapturedInteraction
from ._httpx import install_httpx_interception, uninstall_httpx_interception
from ._openai import OpenAIReplayStore
from ._version import ARTIFACT_VERSION, __version__

DEFAULT_CONFIG_FILENAME: Final[str] = "stagehand.yml"
BUNDLE_VERSION: Final[str] = "v1alpha1"
ENV_SESSION: Final[str] = "STAGEHAND_SESSION"
ENV_MODE: Final[str] = "STAGEHAND_MODE"
ENV_CONFIG_PATH: Final[str] = "STAGEHAND_CONFIG_PATH"
ENV_CAPTURE_OUTPUT: Final[str] = "STAGEHAND_CAPTURE_OUTPUT"
ENV_REPLAY_INPUT: Final[str] = "STAGEHAND_REPLAY_INPUT"

StagehandMode: TypeAlias = Literal["record", "replay", "passthrough"]

VALID_MODES: Final[tuple[StagehandMode, ...]] = ("record", "replay", "passthrough")


class StagehandError(RuntimeError):
    """Base error for Stagehand Python runtime bootstrap failures."""


class AlreadyInitializedError(StagehandError):
    """Raised when the Python SDK is initialized more than once in one process."""


class InvalidModeError(StagehandError):
    """Raised when init() receives an unsupported mode."""


class NotInitializedError(StagehandError):
    """Raised when runtime metadata is requested before init()."""


@dataclass(frozen=True, slots=True)
class RuntimeMetadata:
    session: str
    mode: StagehandMode
    run_id: str
    config_path: str | None
    sdk_version: str
    artifact_version: str
    initialized_at: datetime

    def as_recorder_dict(self) -> dict[str, str | None]:
        return {
            "session": self.session,
            "mode": self.mode,
            "run_id": self.run_id,
            "config_path": self.config_path,
            "sdk_version": self.sdk_version,
            "artifact_version": self.artifact_version,
            "initialized_at": self.initialized_at.isoformat(),
        }


@dataclass(frozen=True, slots=True)
class StagehandRuntime:
    metadata: RuntimeMetadata
    _capture_buffer: CaptureBuffer
    _openai_replay_store: OpenAIReplayStore

    @property
    def session(self) -> str:
        return self.metadata.session

    @property
    def mode(self) -> StagehandMode:
        return self.metadata.mode

    @property
    def run_id(self) -> str:
        return self.metadata.run_id

    @property
    def config_path(self) -> str | None:
        return self.metadata.config_path

    def recorder_metadata(self) -> dict[str, str | None]:
        return self.metadata.as_recorder_dict()

    def captured_interactions(self) -> tuple[CapturedInteraction, ...]:
        return self._capture_buffer.snapshot()

    def captured_interaction_dicts(self) -> list[dict[str, Any]]:
        return [interaction.to_dict() for interaction in self.captured_interactions()]

    def capture_bundle_dict(self) -> dict[str, Any]:
        return {
            "bundle_version": BUNDLE_VERSION,
            "metadata": self.recorder_metadata(),
            "interactions": self.captured_interaction_dicts(),
        }

    def write_capture_bundle(self, path: str | Path) -> str:
        return _write_capture_bundle(path, self.capture_bundle_dict())

    def seed_replay_interactions(
        self,
        interactions: Iterable[CapturedInteraction | dict[str, Any]],
    ) -> int:
        normalized: list[CapturedInteraction] = []
        for interaction in interactions:
            if isinstance(interaction, CapturedInteraction):
                normalized.append(interaction)
            else:
                normalized.append(CapturedInteraction.from_dict(interaction))
        return self._openai_replay_store.seed(normalized)


_runtime_lock = Lock()
_current_runtime: StagehandRuntime | None = None


def init(session: str, mode: str, config_path: str | Path | None = None) -> StagehandRuntime:
    normalized_session = _normalize_session(session)
    normalized_mode = _normalize_mode(mode)
    resolved_config_path = _resolve_config_path(config_path)

    global _current_runtime
    with _runtime_lock:
        if _current_runtime is not None:
            raise AlreadyInitializedError(
                f"Stagehand is already initialized for session {_current_runtime.session!r} "
                f"in mode {_current_runtime.mode!r}."
            )

        metadata = RuntimeMetadata(
            session=normalized_session,
            mode=normalized_mode,
            run_id=f"run_{uuid4().hex[:12]}",
            config_path=resolved_config_path,
            sdk_version=__version__,
            artifact_version=ARTIFACT_VERSION,
            initialized_at=datetime.now(timezone.utc),
        )
        runtime = StagehandRuntime(
            metadata=metadata,
            _capture_buffer=CaptureBuffer(run_id=metadata.run_id),
            _openai_replay_store=OpenAIReplayStore(),
        )
        install_httpx_interception(
            mode=metadata.mode,
            capture_buffer=runtime._capture_buffer,
            replay_store=runtime._openai_replay_store,
        )
        _configure_env_integrations(runtime)
        _current_runtime = runtime
        return runtime


def init_from_env() -> StagehandRuntime:
    session = os.environ.get(ENV_SESSION)
    mode = os.environ.get(ENV_MODE)
    config_path = os.environ.get(ENV_CONFIG_PATH)

    if session is None or mode is None:
        raise ValueError(f"{ENV_SESSION} and {ENV_MODE} must be set for stagehand.init_from_env()")

    return init(session=session, mode=mode, config_path=config_path)


def get_runtime() -> StagehandRuntime:
    if _current_runtime is None:
        raise NotInitializedError("Stagehand has not been initialized in this process.")

    return _current_runtime


def is_initialized() -> bool:
    return _current_runtime is not None


def seed_replay_interactions(
    interactions: Iterable[CapturedInteraction | dict[str, Any]],
) -> int:
    return get_runtime().seed_replay_interactions(interactions)


def _reset_for_tests() -> None:
    global _current_runtime
    with _runtime_lock:
        uninstall_httpx_interception()
        _current_runtime = None


def _configure_env_integrations(runtime: StagehandRuntime) -> None:
    replay_input = os.environ.get(ENV_REPLAY_INPUT)
    if runtime.mode == "replay" and replay_input:
        runtime.seed_replay_interactions(_load_replay_bundle(replay_input))

    capture_output = os.environ.get(ENV_CAPTURE_OUTPUT)
    if capture_output:
        atexit.register(_write_capture_bundle_on_exit, runtime, capture_output)


def _write_capture_bundle_on_exit(runtime: StagehandRuntime, path: str) -> None:
    try:
        runtime.write_capture_bundle(path)
    except Exception as exc:
        print(
            f"stagehand: failed to write capture bundle to {path}: {exc.__class__.__name__}: {exc}",
            file=sys.stderr,
            flush=True,
        )
        return


def _load_replay_bundle(path: str | Path) -> list[dict[str, Any]]:
    raw = Path(path).read_text(encoding="utf-8")
    payload = json.loads(raw)

    if isinstance(payload, list):
        return [dict(item) for item in payload]

    interactions = payload.get("interactions")
    if not isinstance(interactions, list):
        raise ValueError(f"replay bundle at {path!s} does not contain an interactions list")

    return [dict(item) for item in interactions]


def _write_capture_bundle(path: str | Path, payload: dict[str, Any]) -> str:
    target = Path(path).expanduser()
    target.parent.mkdir(parents=True, exist_ok=True)

    with tempfile.NamedTemporaryFile(
        "w",
        delete=False,
        dir=target.parent,
        encoding="utf-8",
        prefix=target.name + ".",
        suffix=".tmp",
    ) as handle:
        json.dump(payload, handle, indent=2)
        handle.write("\n")
        temp_path = Path(handle.name)

    temp_path.replace(target)
    return str(target.resolve())


def _normalize_session(session: str) -> str:
    if not isinstance(session, str):
        raise TypeError("session must be a string")

    normalized = session.strip()
    if not normalized:
        raise ValueError("session must be a non-empty string")

    return normalized


def _normalize_mode(mode: str) -> StagehandMode:
    if not isinstance(mode, str):
        raise TypeError("mode must be a string")

    normalized = mode.strip().lower()
    if normalized not in VALID_MODES:
        allowed = ", ".join(repr(value) for value in VALID_MODES)
        raise InvalidModeError(f"mode must be one of {allowed}")

    return normalized  # type: ignore[return-value]


def _resolve_config_path(config_path: str | Path | None) -> str | None:
    if config_path is None:
        default_path = Path.cwd() / DEFAULT_CONFIG_FILENAME
        if not default_path.is_file():
            return None

        return str(default_path.resolve())

    resolved_path = Path(config_path).expanduser()
    if not resolved_path.is_file():
        raise FileNotFoundError(f"Stagehand config file not found: {resolved_path}")

    return str(resolved_path.resolve())
