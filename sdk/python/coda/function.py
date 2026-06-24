def function(**kwargs):
    def decorator(fn):
        fn._coda_function_kwargs = kwargs
        return fn
    return decorator
