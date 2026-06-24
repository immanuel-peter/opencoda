"""Modal compatibility shim — Phase 2."""

def not_implemented(name: str):
    raise NotImplementedError(
        f"coda.compat: {name} is not implemented in v1. Use the native coda SDK or see docs/compat-gaps.md"
    )
