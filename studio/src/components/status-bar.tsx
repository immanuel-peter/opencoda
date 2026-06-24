"use client";

export function StatusBar() {
  return (
    <div className="flex flex-wrap gap-6 text-xs text-gray-400 border border-surface-border rounded-lg bg-surface-raised/60 px-4 py-3">
      <div>
        <span className="text-gray-500">control plane</span>
        <span className="ml-2 text-accent">connected</span>
      </div>
      <div>
        <span className="text-gray-500">buffer</span>
        <span className="ml-2 text-white">2 warm GPUs</span>
      </div>
      <div>
        <span className="text-gray-500">allocation util</span>
        <span className="ml-2 text-white">—</span>
      </div>
      <div>
        <span className="text-gray-500">object store</span>
        <span className="ml-2 text-white">Garage</span>
      </div>
    </div>
  );
}
