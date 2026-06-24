import { EndpointList } from "@/components/endpoint-list";
import { LogStream } from "@/components/log-stream";
import { StatusBar } from "@/components/status-bar";

export default function Home() {
  return (
    <main className="relative mx-auto max-w-7xl px-6 py-8">
      <header className="mb-10 border-b border-surface-border pb-6">
        <p className="text-xs uppercase tracking-[0.3em] text-accent-muted mb-2">
          opencoda.dev
        </p>
        <h1 className="font-display text-4xl font-bold tracking-tight text-white">
          Studio
        </h1>
        <p className="mt-2 text-sm text-gray-400 max-w-xl">
          Live endpoints, replica states, and streaming inference logs — the
          invisible lifecycle made visible.
        </p>
      </header>

      <StatusBar />

      <div className="mt-8 grid gap-8 lg:grid-cols-5">
        <section className="lg:col-span-2">
          <h2 className="font-display text-lg font-semibold text-accent mb-4">
            Endpoints
          </h2>
          <EndpointList />
        </section>
        <section className="lg:col-span-3">
          <h2 className="font-display text-lg font-semibold text-accent mb-4">
            Live logs
          </h2>
          <LogStream />
        </section>
      </div>
    </main>
  );
}
