import "./globals.css";
import { ThemeProvider } from "@/provider/theme";
import { Toaster } from "@/components/ui/sonner"
import { LocaleProvider } from "@/provider/locale";
import QueryProvider from "@/provider/query";
import { ServiceWorkerRegister } from "@/components/sw-register";
import { TooltipProvider } from "@/components/animate-ui/components/animate/tooltip";
import type { Viewport } from "next";

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};


export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-Hans" suppressHydrationWarning>
      <head>
        <meta name="theme-color" content="#eae9e3" />
        <meta name="application-name" content="Octopus" />
        <meta name="apple-mobile-web-app-capable" content="yes" />
        <meta name="apple-mobile-web-app-status-bar-style" content="black" />
        <meta name="apple-mobile-web-app-title" content="Octopus" />
        <meta name="mobile-web-app-capable" content="yes" />
        <meta name="mobile-web-app-status-bar-style" content="black" />
        <meta name="mobile-web-app-title" content="Octopus" />
        <link rel="manifest" href="./manifest.json" />
        <link rel="icon" href="./favicon.ico" sizes="any" />
        <link rel="apple-touch-icon" href="./apple-icon.png" />
        <title>Octopus</title>
        <style
          dangerouslySetInnerHTML={{
            __html: `
              #initial-loader {
                position: fixed;
                inset: 0;
                z-index: 9999;
                display: flex;
                align-items: center;
                justify-content: center;
                background: hsl(var(--background));
                color: hsl(var(--primary));
                transition: opacity 200ms ease;
              }
              #initial-loader.octo-hide {
                opacity: 0;
                pointer-events: none;
              }
              #initial-loader svg {
                width: 120px;
                height: 120px;
              }
              #initial-loader .octo-group {
                animation: octoFade 2s ease-in-out infinite;
              }
              #initial-loader path {
                fill: none;
                stroke: currentColor;
                stroke-width: 6;
                stroke-linecap: round;
                stroke-dasharray: 1;
                stroke-dashoffset: 1;
                opacity: 0;
                animation: octoDraw 2s ease-in-out infinite both;
              }
              #initial-loader path:nth-child(1) { animation-delay: 0s; }
              #initial-loader path:nth-child(2) { animation-delay: 0.15s; }
              #initial-loader path:nth-child(3) { animation-delay: 0.30s; }
              #initial-loader path:nth-child(4) { animation-delay: 0.45s; }
              #initial-loader path:nth-child(5) { animation-delay: 0.60s; }

              @keyframes octoDraw {
                0%   { stroke-dashoffset: 1; opacity: 0; }
                5%   { opacity: 1; }
                40%  { stroke-dashoffset: 0; opacity: 1; }
                100% { stroke-dashoffset: 0; opacity: 1; }
              }
              @keyframes octoFade {
                0%   { opacity: 1; }
                70%  { opacity: 1; }
                100% { opacity: 0; }
              }

              @media (prefers-reduced-motion: reduce) {
                #initial-loader .octo-group,
                #initial-loader path {
                  animation: none !important;
                  opacity: 1 !important;
                  stroke-dashoffset: 0 !important;
                }
              }
            `,
          }}
        />
      </head>
      <body className="antialiased">
        <div id="initial-loader" role="status" aria-label="Loading">
          <svg viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
            <g className="octo-group">
              <path pathLength="1" d="M50 15 C70 15 85 30 85 50 C85 65 75 75 70 80 M50 15 C30 15 15 30 15 50 C15 65 25 75 30 80" />
              <path pathLength="1" d="M30 80 Q30 90 20 90" />
              <path pathLength="1" d="M43 77 Q43 90 38 90" />
              <path pathLength="1" d="M57 77 Q57 90 62 90" />
              <path pathLength="1" d="M70 80 Q70 90 80 90" />
            </g>
          </svg>
        </div>
        <ServiceWorkerRegister />
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
          <QueryProvider>
            <LocaleProvider>
              <TooltipProvider>
                {children}
                <Toaster />
              </TooltipProvider>
            </LocaleProvider>
          </QueryProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
