import { NextResponse } from "next/server";

export async function GET() {
  return NextResponse.json([
    {
      name: "qwen3-32b-agents",
      ready: 3,
      starting: 1,
      kvHitRate: 0.87,
      model: "hf://Qwen/Qwen3-32B",
    },
  ]);
}
