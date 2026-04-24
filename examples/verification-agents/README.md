# OpenAI Verification Agents

These are intentionally small agents for testing Stagehand's core record, scrub, store, replay, and inspect loop against real OpenAI traffic.

Pinned runtime inputs:

- Python: use the repo/CI Python where possible
- OpenAI Python SDK: `openai==2.32.0`
- Model: `gpt-4o-mini`
- Temperature: `0`
- Stagehand config: `stagehand.verification.yml`

Install the example dependency:

```powershell
python -m pip install -r examples/verification-agents/requirements.txt
```

Run the full verification pass:

```powershell
python examples/verification-agents/verify_openai_agents.py
```

The verifier runs the agents in strict order. It stops at the first failure, checks the SQLite database directly, verifies replay with a dummy replay key, and prints the inspect output path for each scenario. It does not disconnect the machine from the network; replay safety is checked by using seeded replay plus a non-real replay API key so any live dispatch fails instead of masking a replay miss.
