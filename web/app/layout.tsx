import type { Metadata } from 'next';
import './globals.css';
import { AppProviders } from '@/components/providers/AppProviders';

export const metadata: Metadata = {
  title: 'Backup Platform — Operational Console',
  description: 'Enterprise Backup Platform Operational Console',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="antialiased bg-background text-foreground min-h-screen">
        <a href="#main-content" className="skip-to-content">
          Skip to main content
        </a>
        <AppProviders>{children}</AppProviders>
      </body>
    </html>
  );
}
