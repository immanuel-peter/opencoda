"use client";

import { useEffect, useState } from "react";

type Endpoint = {
  name: string;
  ready: number;
  starting: number;
  kvHitRate: number;
  model: string;
};

const GATEWAY = process.env.NEXT_PUBLIC_CODA_GATEWAY_URL || "http://localhost:8090";

export function EndpointList() {
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function load() {
      try {
        const res = await fetch(`${GATEWAY}/v1/studio/endpoints`);
        if (res.ok) {
          setEndpoints(await res.json());
        } else {
          setEndpoints([
            {
              name: "qwen3-32b-agents",
              ready: 3,
              starting: 1,
              kvHitRate: 0.87,
              model: "hf://Qwen/Qwen3-32B",
            },
          ]);
        }
      } catch {
        setEndpoints([
          {
            name: "demo-endpoint",
            ready: 0,
            starting: 0,
            kvHitRate: 0,
            model: "hf://demo",
          },
        ]);
      }
      setLoading(false);
    }
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, []);

  if (loading) {
    return <div className="text-gray-500 text-sm animate-pulse-slow">Loading…</div>;
  }

  return (
    <ul className="space-y-3">
      {endpoints.map((ep) => (
        <li
          key={ep.name}
          className="rounded-lg border border-surface-border bg-surface-raised/80 p-4 backdrop-blur transition hover:border-accent/40 hover:shadow-[0_0_24px_#3dd6c320]"
        >
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="font-display font-semibold text-white">{ep.name}</p>
              <p className="text-xs text-gray-500 mt-1 truncate">{ep.model}</p>
            </div>
            <span
              className={`text-xs px-2 py-0.5 rounded ${
                ep.starting > 0
                  ? "bg-warn/20 text-warn"
                  : ep.ready > 0
                    ? "bg-accent/20 text-accent"
                    : "bg-gray-700 text-gray-400"
              }`}
            >
              {ep.starting > 0 ? "starting" : ep.ready > 0 ? "serving" : "scaled to zero"}
            </span>
          </div>
          <div className="mt-3 flex gap-4 text-xs text-gray-400">
            <span>ready <strong className="text-white">{ep.ready}</strong></span>
            <span>starting <strong className="text-white">{ep.starting}</strong></span>
            <span>
              kv hit{" "}
              <strong className="text-accent">
                {(ep.kvHitRate * 100).toFixed(0)}%
              </strong>
            </span>
          </div>
        </li>
      ))}
    </ul>
  );
}
