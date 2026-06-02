// TradeNexus API Client
import type { SystemHealth, Signal, Watchlist, Stock, Instrument, ScannerMatch, TelegramSettings } from './types';

const API_BASE = '/api';

let authHeader = '';

export function setAuth(username: string, password: string) {
  authHeader = 'Basic ' + btoa(`${username}:${password}`);
}

export function clearAuth() {
  authHeader = '';
}

async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      'Authorization': authHeader,
      ...options.headers,
    },
  });

  if (res.status === 401) {
    throw new Error('Unauthorized');
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || 'Request failed');
  }

  return res.json();
}

// Auth
export async function login(username: string, password: string): Promise<boolean> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) return false;
  const data = await res.json();
  if (data.success) {
    setAuth(username, password);
    return true;
  }
  return false;
}

// Health
export const getHealth = () => apiFetch<SystemHealth>('/health');

// Signals
export const getSignals = (tf?: string, category?: string) => {
  const params = new URLSearchParams();
  if (tf) params.set('timeframe', tf);
  if (category) params.set('category', category);
  const qs = params.toString();
  return apiFetch<Signal[]>(`/signals${qs ? '?' + qs : ''}`);
};

export const getSignalStats = () => apiFetch<Record<string, number>>('/signals/stats');

// Watchlists
export const getWatchlists = () => apiFetch<Watchlist[]>('/watchlists');
export const createWatchlist = (name: string) =>
  apiFetch<Watchlist>('/watchlists', { method: 'POST', body: JSON.stringify({ name }) });
export const updateWatchlist = (id: string, data: Partial<Watchlist>) =>
  apiFetch('/watchlists/' + id, { method: 'PUT', body: JSON.stringify(data) });
export const deleteWatchlist = (id: string) =>
  apiFetch('/watchlists/' + id, { method: 'DELETE' });
export const addStock = (watchlistId: string, stock: Partial<Stock>) =>
  apiFetch<Stock>(`/watchlists/${watchlistId}/stocks`, { method: 'POST', body: JSON.stringify(stock) });
export const removeStock = (watchlistId: string, symbol: string) =>
  apiFetch(`/watchlists/${watchlistId}/stocks/${symbol}`, { method: 'DELETE' });

// Symbols
export const searchSymbols = (q: string) =>
  apiFetch<Instrument[]>(`/symbols/search?q=${encodeURIComponent(q)}`);

// Scanners
export const getScannerResults = () => apiFetch<ScannerMatch[]>('/scanners/results');
export const triggerScanner = () =>
  apiFetch('/scanners/trigger', { method: 'POST' });

// Settings
export const getTelegramSettings = () => apiFetch<TelegramSettings>('/settings/telegram');
export const saveTelegramSettings = (botToken: string, chatId: string) =>
  apiFetch('/settings/telegram', { method: 'POST', body: JSON.stringify({ botToken, chatId }) });
export const testTelegram = (botToken: string, chatId: string) =>
  apiFetch('/settings/telegram/test', { method: 'POST', body: JSON.stringify({ botToken, chatId }) });
