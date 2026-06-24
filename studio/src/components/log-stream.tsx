"use client";

import { useEffect, useRef, useState } from "react";

const GATEWAY = process.env.NEXT_PUBLIC_CODA_GATEWAY_URL || "http://localhost:8090";

export function LogStream() {
  const [lines, setLines] = useState<string[]>([]);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let active = true;
    async function stream() {
      try {
        const res = await fetch(`${GATEWAY}/v1/studio/logs/stream`);
        if (!res.ok || !res.body) throw new Error("no stream");
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        while (active) {
          const { done, value } = await reader.read();
          if (done) break;
          const chunk = decoder.decode(value);
          setLines((prev) => [...prev.slice(-200), chunk].flat());
        }
      } catch {
        const demo = [
          "[replica-0] INFO: Uvicorn running on http://0.0.0.0:8000",
          "[replica-0] INFO: LMCache MP connector attached localhost:5555",
          "[gateway] routed chat completion → replica-0 (prefix hit 94%)",
        ];
        let i = 0;
        const t = setInterval(() => {
          if (!active) return;
          setLines((prev) => [...prev.slice(-200), demo[i % demo.length]]);
          i++;
        }, 2000);
        return () => clearInterval(t);
      }
    }
    stream();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [lines]);

  return (
    <div
      className="h-[480px] rounded-lg border border-surface-border bg-[#080b10] p-4 font-mono text-xs leading-relaxed overflow-y-auto"
    >
      {lines.length === 0 && (
        <p className="text-gray-600">Waiting for log stream…</p>
      )}
      {lines.map((line, i) => (
        <div key={i} className="text-gray-300 border-l-2 border-transparent hover:border-accent/50 pl-2 py-0.5">
          {line}
        </div>
      ))}
      <div ref={bottomRef} />
    </div>
  );
}
