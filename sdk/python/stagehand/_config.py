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

        mappings.append(ServiceMapping(name=name, host=host, path_prefix=path_prefix))

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
            continue

        if not in_services:
            continue

        if indent == 2 and stripped.startswith("- "):
            current = {}
            services.append(current)
            section = None
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
                continue
            section = None
            current[key] = value
            continue

        if indent == 6 and section is not None:
            parent = current.setdefault(section, {})
            if isinstance(parent, dict):
                key, value = _split_key_value(stripped)
                if key:
                    parent[key] = value

    return services


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
