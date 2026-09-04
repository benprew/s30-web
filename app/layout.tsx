import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'Shandalar 30th Anniversary Edition',
  description:
    'A modern, open-source reimagining of the classic Shandalar deck-building adventure.',
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
