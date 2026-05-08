from __future__ import annotations

import json
import random
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True, slots=True)
class ResponseOverride:
    status: int = 0
    body: Any = None
    error: str = ""
    latency_ms: int = 0


@dataclass(frozen=True, slots=True)
class Provenance:
    rule_index: int
    service: str
    operation: str
    call_number: int
    status: int
    name: str = ""
    nth_call: int = 0
    any_call: bool = False
    probability: float | None = None
    library: str = ""
    error: str = ""
    latency_ms: int = 0

    def to_dict(self) -> dict[str, Any]:
        data: dict[str, Any] = {
            "rule_index": self.rule_index,
            "service": self.service,
            "operation": self.operation,
            "call_number": self.call_number,
        }
        if self.status:
            data["status"] = self.status
        if self.name:
            data["name"] = self.name
        if self.nth_call > 0:
            data["nth_call"] = self.nth_call
        if self.any_call:
            data["any_call"] = self.any_call
        if self.probability is not None:
            data["probability"] = self.probability
        if self.library:
            data["library"] = self.library
        if self.error:
            data["error"] = self.error
        if self.latency_ms:
            data["latency_ms"] = self.latency_ms
        return data


@dataclass(frozen=True, slots=True)
class Decision:
    override: ResponseOverride
    provenance: Provenance


@dataclass(frozen=True, slots=True)
class _Rule:
    name: str
    service: str
    operation: str
    nth_call: int
    any_call: bool
    probability: float | None
    library: str
    status: int
    error: str
    latency_ms: int
    body: Any


class InjectionEngine:
    def __init__(self, rules: list[_Rule]) -> None:
        self._rules = rules
        self._counts: dict[tuple[str, str], int] = {}
        self._applied: list[Provenance] = []
        self._rng = random.Random(1)

    def evaluate(self, *, service: str, operation: str) -> Decision | None:
        service = service.strip()
        operation = operation.strip()
        key = (service, operation)
        call_number = self._counts.get(key, 0) + 1
        self._counts[key] = call_number

        for idx, rule in enumerate(self._rules):
            if rule.service != service or rule.operation != operation:
                continue
            if rule.nth_call > 0 and rule.nth_call != call_number:
                continue
            if rule.probability is not None:
                if rule.probability <= 0:
                    continue
                if rule.probability < 1 and self._rng.random() >= rule.probability:
                    continue

            provenance = Provenance(
                rule_index=idx,
                service=service,
                operation=operation,
                call_number=call_number,
                name=rule.name,
                nth_call=rule.nth_call,
                any_call=rule.any_call,
                probability=rule.probability,
                library=rule.library,
                status=rule.status,
                error=rule.error,
                latency_ms=rule.latency_ms,
            )
            self._applied.append(provenance)
            return Decision(
                override=ResponseOverride(
                    status=rule.status,
                    body=_clone_value(rule.body),
                    error=rule.error,
                    latency_ms=rule.latency_ms,
                ),
                provenance=provenance,
            )

        return None

    def metadata(self) -> dict[str, Any]:
        if not self._applied:
            return {}
        return {"error_injection": {"applied": [item.to_dict() for item in self._applied]}}


def load_engine(path: str | Path | None) -> InjectionEngine:
    if path is None or str(path).strip() == "":
        return InjectionEngine([])

    payload = json.loads(Path(path).read_text(encoding="utf-8"))
    rules = []
    for raw_rule in payload.get("rules", []):
        match = raw_rule.get("match", {})
        inject = raw_rule.get("inject", {})
        rules.append(
            _Rule(
                name=str(raw_rule.get("name", "")).strip(),
                service=str(match.get("service", "")).strip(),
                operation=str(match.get("operation", "")).strip(),
                nth_call=int(match.get("nth_call", 0) or 0),
                any_call=bool(match.get("any_call", False)),
                probability=inject_probability(match.get("probability")),
                library=str(inject.get("library", "")).strip(),
                status=int(inject.get("status", 0) or 0),
                error=str(inject.get("error", "")).strip(),
                latency_ms=int(inject.get("latency_ms", 0) or 0),
                body=_clone_value(inject.get("body")),
            )
        )
    return InjectionEngine(rules)


def inject_probability(value: Any) -> float | None:
    if value is None:
        return None
    return float(value)


def _clone_value(value: Any) -> Any:
    if isinstance(value, dict):
        return {key: _clone_value(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_clone_value(item) for item in value]
    return value
