from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit


@dataclass(frozen=True, slots=True)
class ServiceMapping:
    name: str
    host: str
    path_prefix: str
    allowed_tiers: tuple[int, ...] = (0,)
    ignore_request_paths: tuple[str, ...] = ()
    ignore_response_paths: tuple[str, ...] = ()

    def matches(self, url: str) -> bool:
        parsed = urlsplit(url)
        if (parsed.hostname or "").lower() != self.host:
            return False
        return (parsed.path or "/").startswith(self.path_prefix)


def load_service_mappings(config_path: str | Path | None) -> tuple[ServiceMapping, ...]:
    if config_path is None:
        return ()

    try:
        raw = Path(config_path).read_text(encoding="utf-8")
    except OSError:
        return ()

    mappings: list[ServiceMapping] = []
    for service in _parse_services(raw):
        name = str(service.get("name", "")).strip()
        match = service.get("match")
        if not name or not isinstance(match, dict):
            continue

        host = str(match.get("host", "")).strip().lower()
        if not host:
            continue

        path_prefix = str(match.get("path_prefix", "/")).strip() or "/"
        if not path_prefix.startswith("/"):
            path_prefix = f"/{path_prefix}"

        ignore = service.get("ignore")
        ignore_request_paths: tuple[str, ...] = ()
        ignore_response_paths: tuple[str, ...] = ()
        if isinstance(ignore, dict):
            ignore_request_paths = _string_tuple(ignore.get("request_paths"))
            ignore_response_paths = _string_tuple(ignore.get("response_paths"))

        replay = service.get("replay")
        allowed_tiers = (0,)
        if isinstance(replay, dict):
            configured_tiers = _int_tuple(replay.get("allowed_tiers"))
            if configured_tiers:
                allowed_tiers = configured_tiers

        mappings.append(
            ServiceMapping(
                name=name,
                host=host,
                path_prefix=path_prefix,
                allowed_tiers=allowed_tiers,
                ignore_request_paths=ignore_request_paths,
                ignore_response_paths=ignore_response_paths,
            )
        )

    return tuple(
        sorted(
            mappings,
            key=lambda mapping: (mapping.host, len(mapping.path_prefix), mapping.name),
            reverse=True,
        )
    )


def _parse_services(raw: str) -> list[dict[str, Any]]:
    services: list[dict[str, Any]] = []
    current: dict[str, Any] | None = None
    section: str | None = None
    list_key: str | None = None
    in_services = False

    for raw_line in raw.splitlines():
        line = _strip_comment(raw_line).rstrip()
        if not line.strip():
            continue

        indent = len(line) - len(line.lstrip(" "))
        stripped = line.strip()
        if indent == 0:
            in_services = stripped == "services:"
            current = None
            section = None
            list_key = None
            continue

        if not in_services:
            continue

        if indent == 2 and stripped.startswith("- "):
            current = {}
            services.append(current)
            section = None
            list_key = None
            remainder = stripped[2:].strip()
            if remainder:
                key, value = _split_key_value(remainder)
                if key:
                    current[key] = value
            continue

        if current is None:
            continue

        if indent == 4:
            key, value = _split_key_value(stripped)
            if not key:
                continue
            if value is None:
                section = key
                current.setdefault(section, {})
                list_key = None
                continue
            section = None
            list_key = None
            current[key] = value
            continue

        if indent == 6 and section is not None:
            parent = current.setdefault(section, {})
            if isinstance(parent, dict):
                key, value = _split_key_value(stripped)
                if key:
                    if value is None:
                        parent.setdefault(key, [])
                        list_key = key
                    else:
                        parent[key] = value
                        list_key = None
            continue

        if indent == 8 and section is not None and list_key is not None:
            parent = current.setdefault(section, {})
            if isinstance(parent, dict) and stripped.startswith("- "):
                items = parent.setdefault(list_key, [])
                if isinstance(items, list):
                    items.append(_parse_scalar(stripped[2:].strip()))

    return services


def _string_tuple(value: Any) -> tuple[str, ...]:
    if not isinstance(value, list):
        return ()
    return tuple(str(item).strip() for item in value if str(item).strip())


def _int_tuple(value: Any) -> tuple[int, ...]:
    if not isinstance(value, list):
        return ()
    tiers: list[int] = []
    for item in value:
        if isinstance(item, int) and item not in tiers:
            tiers.append(item)
    return tuple(tiers)


def _split_key_value(value: str) -> tuple[str | None, Any]:
    if ":" not in value:
        return None, None

    key, raw = value.split(":", 1)
    key = key.strip()
    raw = raw.strip()
    if not key:
        return None, None
    if not raw:
        return key, None
    return key, _parse_scalar(raw)


def _parse_scalar(value: str) -> Any:
    if value.startswith("[") and value.endswith("]"):
        inner = value[1:-1].strip()
        if not inner:
            return []
        return [_parse_scalar(item.strip()) for item in inner.split(",")]
    if (value.startswith('"') and value.endswith('"')) or (
        value.startswith("'") and value.endswith("'")
    ):
        return value[1:-1]
    if value.isdigit():
        return int(value)
    return value


def _strip_comment(line: str) -> str:
    in_single = False
    in_double = False
    for index, char in enumerate(line):
        if char == "'" and not in_double:
            in_single = not in_single
        elif char == '"' and not in_single:
            in_double = not in_double
        elif char == "#" and not in_single and not in_double:
            return line[:index]
    return line
