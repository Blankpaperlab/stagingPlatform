import openai

from _stagehand_agent_common import MODEL, init_stagehand


init_stagehand("test-1-simple")

client = openai.OpenAI()
response = client.chat.completions.create(
    model=MODEL,
    messages=[
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": "What is the capital of France?"},
    ],
    temperature=0,
)

print(response.choices[0].message.content or "")
