import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Skills Registry · One GitHub repo, every AI agent",
  description: "Keep agent skills in a GitHub repo you own. Discover, search, sync, and share them with a Go CLI and TUI, gateway skill, or native macOS app.",
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
