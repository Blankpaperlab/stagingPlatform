import openai

from _stagehand_agent_common import MODEL, init_stagehand


init_stagehand("test-3-sensitive")

SENSITIVE_PROMPT = """
Please summarize the following customer support ticket:

Customer email: john.doe@example.com
Customer phone: +1-415-555-2671
Account number: 4532-1488-0343-6464
SSN on file: 123-45-6789
API token they shared: sk-proj-abc123def456ghi789jkl012mno345
Password they accidentally pasted: hunter2

Issue: Customer is locked out of their account and needs help resetting credentials.
"""

client = openai.OpenAI()
response = client.chat.completions.create(
    model=MODEL,
    messages=[
        {
            "role": "system",
            "content": "You are a customer support assistant. Summarize tickets.",
        },
        {"role": "user", "content": SENSITIVE_PROMPT},
    ],
    temperature=0,
)

print(response.choices[0].message.content or "")
