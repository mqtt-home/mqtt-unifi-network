import { useMemo } from 'react';
import { Sun, Moon, Wifi, WifiOff, Cable, Home, DoorOpen, SignalHigh, SignalMedium, SignalLow, AlertTriangle } from 'lucide-react';
import { useSSE } from '@/hooks/useSSE';
import { useTheme } from '@/contexts/ThemeContext';
import { sortFloors, formatRelative, signalQuality } from '@/types/presence';
import type { ClientState } from '@/types/presence';

export function App() {
  const { snapshot, isConnected, error, reconnect } = useSSE();
  const { theme, toggleTheme } = useTheme();

  const { online, offline, floors } = useMemo(() => {
    const clients = snapshot?.clients ?? [];
    return {
      online: clients.filter(c => c.online),
      offline: clients.filter(c => !c.online),
      floors: sortFloors(Object.keys(snapshot?.floors ?? {})),
    };
  }, [snapshot]);

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-10 border-b bg-background/95 backdrop-blur">
        <div className="mx-auto flex max-w-3xl items-center justify-between p-4">
          <div className="flex items-center gap-3">
            <h1 className="text-lg font-semibold">UniFi Presence</h1>
            {snapshot && (
              <span className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${
                snapshot.anyone_home
                  ? 'bg-green-500/15 text-green-600 dark:text-green-400'
                  : 'bg-muted text-muted-foreground'
              }`}>
                {snapshot.anyone_home ? <Home className="h-3.5 w-3.5" /> : <DoorOpen className="h-3.5 w-3.5" />}
                {snapshot.anyone_home ? 'Someone home' : 'Nobody home'}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={reconnect}
              title={isConnected ? 'Live' : (error ?? 'Disconnected')}
              className="touch-target flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground"
            >
              {isConnected
                ? <Wifi className="h-5 w-5 text-green-500" />
                : <WifiOff className="h-5 w-5 text-red-500" />}
            </button>
            <button
              onClick={toggleTheme}
              title="Toggle theme"
              className="touch-target flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground"
            >
              {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-3xl space-y-6 p-4">
        {snapshot === null ? (
          <div className="text-muted-foreground">Waiting for status…</div>
        ) : (
          <>
            {snapshot.forbidden ? (
              <div className="flex items-start gap-2 rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-400">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>
                  The UniFi account has no access to the Network application (HTTP 403).
                  Grant it the <strong>Network → View Only</strong> role, or use a dedicated account.
                </span>
              </div>
            ) : !snapshot.connected && (
              <div className="flex items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400">
                <AlertTriangle className="h-4 w-4 shrink-0" />
                Controller unreachable — presence below is the last known state, not live.
              </div>
            )}

            {floors.length > 0 && (
              <section>
                <h2 className="mb-2 text-sm font-medium text-muted-foreground">Floors</h2>
                <div className="grid grid-cols-3 gap-2">
                  {floors.map(floor => (
                    <div key={floor} className="rounded-lg border bg-card p-3 text-card-foreground">
                      <div className="text-xs uppercase tracking-wide text-muted-foreground">{floor}</div>
                      <div className="text-2xl font-semibold">{snapshot.floors[floor].count}</div>
                      <div className="truncate text-xs text-muted-foreground">
                        {snapshot.floors[floor].clients.join(', ') || '—'}
                      </div>
                    </div>
                  ))}
                </div>
              </section>
            )}

            <section>
              <h2 className="mb-2 text-sm font-medium text-muted-foreground">
                Home ({online.length})
              </h2>
              {online.length === 0 ? (
                <div className="rounded-lg border bg-card p-4 text-sm text-muted-foreground">
                  Nobody is currently on the network.
                </div>
              ) : (
                <div className="space-y-2">
                  {online.map(client => <ClientRow key={client.slug} client={client} />)}
                </div>
              )}
            </section>

            {offline.length > 0 && (
              <section>
                <h2 className="mb-2 text-sm font-medium text-muted-foreground">
                  Away ({offline.length})
                </h2>
                <div className="space-y-2">
                  {offline.map(client => <ClientRow key={client.slug} client={client} />)}
                </div>
              </section>
            )}
          </>
        )}
      </main>
    </div>
  );
}

function ClientRow({ client }: { client: ClientState }) {
  const quality = signalQuality(client.signal);

  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border bg-card p-3 text-card-foreground">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className={`h-2 w-2 shrink-0 rounded-full ${client.online ? 'bg-green-500' : 'bg-muted-foreground/40'}`} />
          <span className="truncate font-medium">{client.name}</span>
          {client.floor && (
            <span className="rounded bg-secondary px-1.5 py-0.5 text-xs uppercase text-secondary-foreground">
              {client.floor}
            </span>
          )}
          {client.wired && <Cable className="h-3.5 w-3.5 text-muted-foreground" />}
        </div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          {client.online
            ? (client.associated
                // The device is on the network right now.
                ? `${client.ap || 'unknown AP'}${client.essid ? ` · ${client.essid}` : ''}`
                // Held online by the away delay: radio asleep, not gone.
                : `radio asleep · last seen ${formatRelative(client.last_seen)}`)
            : `away since ${formatRelative(client.since)}`}
        </div>
      </div>

      {client.associated && quality !== 'unknown' && (
        <div
          className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground"
          title={quality === 'weak'
            ? `${client.signal} dBm — weak, the floor may come from an AP on another floor`
            : `${client.signal} dBm (rssi ${client.rssi})`}
        >
          {quality === 'good' && <SignalHigh className="h-4 w-4 text-green-500" />}
          {quality === 'ok' && <SignalMedium className="h-4 w-4 text-amber-500" />}
          {quality === 'weak' && <SignalLow className="h-4 w-4 text-red-500" />}
          {client.signal} dBm
        </div>
      )}
    </div>
  );
}
