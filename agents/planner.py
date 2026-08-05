#!/usr/bin/env python3
"""Deterministic placeholder for the future scenario-planning Agent.

Input and output are JSON on stdin/stdout. This process proposes goals only;
it cannot execute events, mark coverage, or decide whether a violation is real.
"""

from __future__ import annotations

import json
import sys
from typing import Any


def propose(request: dict[str, Any]) -> dict[str, Any]:
    uncovered = request.get("uncovered_atoms", [])
    if not uncovered:
        return {"version": 1, "proposals": [], "reason": "no uncovered atoms supplied"}

    atom = uncovered[0]
    atom_id = atom.get("id", "unknown") if isinstance(atom, dict) else str(atom)
    return {
        "version": 1,
        "proposals": [
            {
                "goal": atom_id,
                "constraints": [],
                "confidence": 0.0,
                "requires_review": True,
            }
        ],
        "reason": "placeholder selects the first coverage debt deterministically",
    }


def main() -> None:
    request = json.load(sys.stdin)
    json.dump(propose(request), sys.stdout, ensure_ascii=False, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
