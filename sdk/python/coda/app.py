class App:
    def __init__(self, name: str):
        self.name = name
        self._functions = []

    def function(self, **kwargs):
        def decorator(fn):
            self._functions.append((fn, kwargs))
            return fn
        return decorator
