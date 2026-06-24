import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "OpenCoda Studio",
  description: "GPU inference control plane",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen font-mono">
        <div className="fixed inset-0 pointer-events-none bg-gradient-to-b from-accent/5 via-transparent to-transparent" />
        {children}
      </body>
    </html>
  );
}
