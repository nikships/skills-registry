import type { Metadata, Viewport } from "next";
import "./globals.css";

const siteUrl = "https://skills-registry.web.app";
const siteTitle = "Skills Registry · One GitHub repo, every AI agent";
const siteDescription =
  "Keep agent skills in a GitHub repo you own. Discover, search, sync, and share them with a Go CLI and TUI, gateway skill, or native macOS app.";

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: siteTitle,
  description: siteDescription,
  alternates: {
    canonical: "/",
  },
  openGraph: {
    type: "website",
    url: "/",
    siteName: "Skills Registry",
    title: siteTitle,
    description: siteDescription,
    images: [
      {
        url: "/og.png",
        width: 1200,
        height: 630,
        alt: "Skills Registry — One GitHub repo. Every AI agent.",
      },
    ],
  },
  twitter: {
    card: "summary_large_image",
    title: siteTitle,
    description: siteDescription,
    images: ["/og.png"],
  },
};

export const viewport: Viewport = {
  themeColor: "#000000",
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
