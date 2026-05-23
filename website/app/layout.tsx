import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "skills-registry · One GitHub repo, every AI agent",
  description: "Stop copy-pasting SKILL.md files into ~/.claude, ~/.cursor, ~/.codex. Skills live in one repo you own. Agents fetch them on demand over MCP — no startup-token tax, no drift.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
