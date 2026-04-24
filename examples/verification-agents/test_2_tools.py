import json

import openai

from _stagehand_agent_common import MODEL, init_stagehand


init_stagehand("test-2-tools")


def get_current_weather(location: str) -> dict[str, object]:
    return {"location": location, "temperature": 72, "condition": "sunny"}


def get_weather_advice(condition: str) -> str:
    advice = {
        "sunny": "Wear sunscreen and bring water.",
        "rainy": "Bring an umbrella.",
        "cold": "Wear a jacket.",
    }
    return advice.get(condition, "Dress comfortably.")


tools = [
    {
        "type": "function",
        "function": {
            "name": "get_current_weather",
            "description": "Get current weather for a location",
            "parameters": {
                "type": "object",
                "properties": {"location": {"type": "string"}},
                "required": ["location"],
            },
        },
    },
    {
        "type": "function",
        "function": {
            "name": "get_weather_advice",
            "description": "Get advice based on weather condition",
            "parameters": {
                "type": "object",
                "properties": {"condition": {"type": "string"}},
                "required": ["condition"],
            },
        },
    },
]

client = openai.OpenAI()
messages: list[dict[str, object]] = [
    {"role": "system", "content": "You are a weather assistant."},
    {
        "role": "user",
        "content": "What's the weather in Paris and what should I wear?",
    },
]
tool_plan = ["get_current_weather", "get_weather_advice", None]

for tool_name in tool_plan:
    request: dict[str, object] = {
        "model": MODEL,
        "messages": messages,
        "tools": tools,
        "temperature": 0,
    }
    if tool_name is None:
        request["tool_choice"] = "none"
    else:
        request["tool_choice"] = {"type": "function", "function": {"name": tool_name}}

    response = client.chat.completions.create(**request)
    msg = response.choices[0].message
    messages.append(msg.model_dump(exclude_none=True))

    if not msg.tool_calls:
        print(msg.content or "")
        break

    for tool_call in msg.tool_calls:
        args = json.loads(tool_call.function.arguments)
        if tool_call.function.name == "get_current_weather":
            result = get_current_weather(args["location"])
        elif tool_call.function.name == "get_weather_advice":
            result = get_weather_advice(args["condition"])
        else:
            result = {"error": "unknown function"}

        messages.append(
            {
                "role": "tool",
                "tool_call_id": tool_call.id,
                "content": json.dumps(result),
            }
        )
