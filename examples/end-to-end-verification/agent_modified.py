from __future__ import annotations

import os

from agent import main


if __name__ == "__main__":
    os.environ.setdefault("STAGEHAND_REFUND_PROMPT_STYLE", "careful")
    raise SystemExit(main())
