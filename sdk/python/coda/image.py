class Image:
    def __init__(self, base: str):
        self.base = base

    def pip_install(self, packages: list[str]) -> "Image":
        self._pip = packages
        return self
