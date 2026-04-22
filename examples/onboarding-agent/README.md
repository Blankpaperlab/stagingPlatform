# Onboarding Agent Example

Current example files:

- `openai_demo_agent.py` demonstrates a minimal streaming OpenAI onboarding flow over `httpx`

Current scope:

- streams a chat-completions request with a tool definition
- exercises Stagehand's D3 OpenAI SSE capture path
- is used by the Python integration test to prove exact replay can run offline through the same client surface

Deferred:

- Stripe customer creation
- follow-up side effects
- multi-service onboarding flow
