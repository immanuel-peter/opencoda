import os
from typing import Optional

import httpx


class Client:
    """Gateway client using CODA_SERVER_URL + token pair."""

    def __init__(
        self,
        server_url: Optional[str] = None,
        token_id: Optional[str] = None,
        token_secret: Optional[str] = None,
    ):
        self.server_url = (server_url or os.environ.get("CODA_SERVER_URL", "")).rstrip("/")
        self.token_id = token_id or os.environ.get("CODA_TOKEN_ID", "")
        self.token_secret = token_secret or os.environ.get("CODA_TOKEN_SECRET", "")

    def _headers(self) -> dict:
        if self.token_id and self.token_secret:
            return {"Authorization": f"Bearer {self.token_id}:{self.token_secret}"}
        return {}

    def deploy(self, name: str, spec: dict) -> dict:
        with httpx.Client(base_url=self.server_url, headers=self._headers()) as c:
            r = c.post(f"/v1/deploy/{name}", json=spec)
            r.raise_for_status()
            return r.json()

    def logs(self, endpoint: str, follow: bool = True) -> None:
        url = f"{self.server_url}/v1/logs/{endpoint}"
        with httpx.Client(headers=self._headers(), timeout=None) as c:
            with c.stream("GET", url) as r:
                for line in r.iter_lines():
                    print(line)

    def scale(self, endpoint: str, replicas: int) -> dict:
        with httpx.Client(base_url=self.server_url, headers=self._headers()) as c:
            r = c.post(f"/v1/scale/{endpoint}", json={"replicas": replicas})
            r.raise_for_status()
            return r.json()
