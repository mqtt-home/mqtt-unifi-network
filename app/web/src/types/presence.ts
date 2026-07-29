// Mirrors the Go types in app/unifi/types.go. The same shapes go out over MQTT
// (`<topic>/clients/<slug>`, `<topic>/floors/<floor>`) and over SSE.

export interface ClientState {
  name: string;
  slug: string;
  mac: string;
  online: boolean;
  floor?: string;
  ap?: string;
  ap_mac?: string;
  /** UniFi's signal-above-noise figure: higher is better, not dBm. */
  rssi?: number;
  /** Received power in dBm, therefore negative. */
  signal?: number;
  essid?: string;
  ip?: string;
  wired: boolean;
  associated: boolean;
  last_seen?: string;
  since?: string;
}

export interface FloorState {
  count: number;
  clients: string[];
}

export interface Snapshot {
  clients: ClientState[];
  floors: Record<string, FloorState>;
  anyone_home: boolean;
  connected: boolean;
  /** The controller answered 403 — the account is missing the Network role. */
  forbidden: boolean;
  updated_at: string;
}

// Floors read naturally bottom-to-top; anything unrecognised sorts after.
const FLOOR_ORDER = ['ug', 'eg', 'og', 'dg'];

export function sortFloors(floors: string[]): string[] {
  return [...floors].sort((a, b) => {
    const ia = FLOOR_ORDER.indexOf(a);
    const ib = FLOOR_ORDER.indexOf(b);
    if (ia !== -1 && ib !== -1) return ia - ib;
    if (ia !== -1) return -1;
    if (ib !== -1) return 1;
    return a.localeCompare(b);
  });
}

export function formatRelative(iso?: string): string {
  if (!iso) return 'never';
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return 'unknown';

  const seconds = Math.floor((Date.now() - then) / 1000);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  return `${Math.floor(hours / 24)} d ago`;
}

// Judge signal from `signal` (dBm, negative) — NOT from `rssi`, which UniFi
// reports as a positive signal-above-noise figure. Below about -75 dBm a client
// is often attached to an access point on another floor, so the floor reading
// deserves a second look.
export function signalQuality(signal?: number): 'good' | 'ok' | 'weak' | 'unknown' {
  if (signal === undefined || signal === 0) return 'unknown';
  if (signal > -60) return 'good';
  if (signal > -75) return 'ok';
  return 'weak';
}
