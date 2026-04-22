from __future__ import annotations

from typing import Any

import httpx

OPENAI_CHAT_COMPLETIONS_URL = "https://api.openai.com/v1/chat/completions"


def run_onboarding_agent(client: httpx.Client) -> dict[str, Any]:
    payload = {
        "model": "gpt-5.4",
        "stream": True,
        "messages": [
            {
                "role": "system",
                "content": "You are an onboarding assistant that can look up workspace setup steps.",
            },
            {
                "role": "user",
                "content": "Help a new customer connect billing and fetch account setup steps.",
            },
        ],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "lookup_workspace",
                    "description": "Look up onboarding steps for a workspace.",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "workspace_id": {"type": "string"},
                        },
                        "required": ["workspace_id"],
                    },
                },
            }
        ],
    }
    headers = {"authorization": "Bearer test-key"}

    with client.stream("POST", OPENAI_CHAT_COMPLETIONS_URL, headers=headers, json=payload) as response:
        response.raise_for_status()
        lines = [line for line in response.iter_lines() if line]

    return {
        "status_code": response.status_code,
        "lines": lines,
    }
