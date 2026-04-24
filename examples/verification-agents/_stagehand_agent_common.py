import os

import stagehand


MODEL = "gpt-4o-mini"


def init_stagehand(default_session: str) -> stagehand.StagehandRuntime:
    if os.getenv("STAGEHAND_SESSION") and os.getenv("STAGEHAND_MODE"):
        return stagehand.init_from_env()

    return stagehand.init(session=default_session, mode="record")
