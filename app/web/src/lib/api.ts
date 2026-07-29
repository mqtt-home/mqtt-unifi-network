import type { Snapshot } from '@/types/presence';

// In `pnpm run dev` Vite serves the UI on :5173 while the Go backend stays on
// :8080; in production both are the same origin.
export const API_BASE = import.meta.env.DEV ? 'http://localhost:8080/api' : '/api';

export async function fetchSnapshot(): Promise<Snapshot> {
  const response = await fetch(`${API_BASE}/status`);
  if (!response.ok) throw new Error('Failed to fetch status');
  return response.json();
}
