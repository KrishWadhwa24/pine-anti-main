import { useState, useEffect, useCallback, useMemo } from 'react';
import * as api from './api';
import type { SystemHealth, Signal, Watchlist, Instrument, TelegramSettings } from './types';

type Page = 'dashboard' | 'signals' | 'watchlists' | 'scanners' | 'settings';

type QuoteWSMessage = {
  type: 'quotes';
  quotes: Array<{ token: string; price: number; timestamp: number }>;
};

export default function App() {
  const [authed, setAuthed] = useState(false);
  const [page, setPage] = useState<Page>(() => {
    return (localStorage.getItem('tn_page') as Page) || 'dashboard';
  });
  const [sidebarOpen, setSidebarOpen] = useState(false);

  // Check stored credentials
  useEffect(() => {
    const saved = localStorage.getItem('tn_auth');
    if (saved) {
      const { u, p } = JSON.parse(saved);
      api.setAuth(u, p);
      setAuthed(true);
    }
  }, []);

  useEffect(() => {
    localStorage.setItem('tn_page', page);
  }, [page]);

  if (!authed) {
    return <LandingPage onLogin={(u, p) => {
      localStorage.setItem('tn_auth', JSON.stringify({ u, p }));
      setAuthed(true);
    }} />;
  }

  return (
    <div className="app-layout">
      <Sidebar page={page} setPage={setPage} isOpen={sidebarOpen} setOpen={setSidebarOpen} onLogout={() => {
        api.clearAuth();
        localStorage.removeItem('tn_auth');
        localStorage.removeItem('tn_page');
        setAuthed(false);
        setPage('dashboard');
      }} />
      <main className={`main-content ${sidebarOpen ? 'sidebar-open' : ''}`}>
        <div className="desktop-header">
          <button className="btn-icon" style={{ border: 'none', background: 'transparent', fontSize: 24, padding: 0 }} 
            onClick={() => setSidebarOpen(!sidebarOpen)}
            onMouseEnter={() => setSidebarOpen(true)}>
            ☰
          </button>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <img src="/assets/logo.svg" alt="TradeNexus Logo" className="sidebar-brand-icon" style={{ width: 36, height: 36, background: 'transparent' }} />
            <span style={{ fontWeight: 700, fontSize: 16 }}>TradeNexus</span>
          </div>
        </div>

        <div className="mobile-header">
          <img src="/assets/logo.svg" alt="TradeNexus Logo" className="sidebar-brand-icon" style={{ width: 36, height: 36, background: 'transparent' }} />
          <span style={{ fontWeight: 700, fontSize: 16 }}>TradeNexus</span>
        </div>
        {page === 'dashboard' && <DashboardPage />}
        {page === 'signals' && <SignalsPage />}
        {page === 'watchlists' && <WatchlistsPage />}
        {page === 'scanners' && <ScannersPage />}
        {page === 'settings' && <SettingsPage />}

        {/* Mobile Bottom Navigation */}
        <nav className="mobile-nav">
          <button className={`mobile-nav-item ${page === 'dashboard' ? 'active' : ''}`} onClick={() => setPage('dashboard')}>
            <span className="mobile-nav-icon">◉</span><span className="mobile-nav-label">Dash</span>
          </button>
          <button className={`mobile-nav-item ${page === 'signals' ? 'active' : ''}`} onClick={() => setPage('signals')}>
            <span className="mobile-nav-icon">∿</span><span className="mobile-nav-label">Signals</span>
          </button>
          <button className={`mobile-nav-item ${page === 'watchlists' ? 'active' : ''}`} onClick={() => setPage('watchlists')}>
            <span className="mobile-nav-icon">☰</span><span className="mobile-nav-label">Watch</span>
          </button>
          <button className={`mobile-nav-item ${page === 'scanners' ? 'active' : ''}`} onClick={() => setPage('scanners')}>
            <span className="mobile-nav-icon">⊛</span><span className="mobile-nav-label">Scan</span>
          </button>
          <button className={`mobile-nav-item ${page === 'settings' ? 'active' : ''}`} onClick={() => setPage('settings')}>
            <span className="mobile-nav-icon">⚙</span><span className="mobile-nav-label">Settings</span>
          </button>
        </nav>
      </main>
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// LANDING PAGE
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function LandingPage({ onLogin }: { onLogin: (u: string, p: string) => void }) {
  const [showLogin, setShowLogin] = useState(false);

  return (
    <div className="landing-page">
      <nav className="landing-nav">
        <div className="sidebar-brand" style={{ margin: 0 }}>
          <img src="/assets/logo.svg" alt="TradeNexus Logo" className="sidebar-brand-icon" style={{ width: 44, height: 44, background: 'transparent' }} />
          <div className="sidebar-brand-text">TradeNexus</div>
        </div>
        <button className="btn btn-secondary" onClick={() => setShowLogin(true)}>Sign In</button>
      </nav>

      <main className="landing-hero">
        <h1 className="landing-title">Find winning stocks before the crowd.</h1>
        <p className="landing-subtitle">
          Proprietary scanners that surface outperforming NSE & BSE stocks — so you act on data, not tips.
        </p>

        <div className="landing-features">
          <div className="landing-feature-pill">100% Free Forever</div>
          <div className="landing-feature-pill">Real-time Telegram Alerts</div>
          <div className="landing-feature-pill">PineScript Engine</div>
        </div>

        <button className="btn btn-primary" style={{ padding: '16px 32px', fontSize: 18, borderRadius: 30 }}
          onClick={() => setShowLogin(true)}>
          Start scanning free
        </button>
        <p style={{ marginTop: 16, fontSize: 13, color: 'var(--text-tertiary)' }}>Trusted by traders. No credit card required.</p>

        {/* Mockup Return Display */}
        <div className="landing-returns-wrapper">
          <p className="landing-returns-title">RETURNS GENERATED AFTER CROSSOVER SIGNAL TRIGGERED</p>
          <div className="landing-returns-grid">
            <div className="return-item"><span className="symbol">TARIL</span><span className="gain">+4430%</span></div>
            <div className="return-item"><span className="symbol">E2E</span><span className="gain">+2700%</span></div>
            <div className="return-item"><span className="symbol">RVNL</span><span className="gain">+1848%</span></div>
            <div className="return-item"><span className="symbol">SHAKTI</span><span className="gain">+1750%</span></div>
          </div>
        </div>
      </main>

      {showLogin && (
        <div className="modal-overlay" onClick={() => setShowLogin(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <LoginScreen onLogin={onLogin} />
          </div>
        </div>
      )}
    </div>
  );
}

function LoginScreen({ onLogin }: { onLogin: (u: string, p: string) => void }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      const ok = await api.login(username, password);
      if (ok) onLogin(username, password);
      else setError('Invalid credentials');
    } catch {
      setError('Connection failed');
    }
    setLoading(false);
  };

  return (
    <div style={{ width: '100%' }}>
      <h2 className="modal-title" style={{ textAlign: 'center', marginBottom: 24 }}>Welcome Back</h2>
      <form className="login-form" onSubmit={handleSubmit}>
        {error && <div className="login-error">{error}</div>}
        <div className="input-group">
          <label className="input-label">Username</label>
          <input className="input" type="text" value={username}
            onChange={e => setUsername(e.target.value)} autoFocus placeholder="admin" />
        </div>
        <div className="input-group">
          <label className="input-label">Password</label>
          <input className="input" type="password" value={password}
            onChange={e => setPassword(e.target.value)} placeholder="••••••••" />
        </div>
        <button className="btn btn-primary" type="submit" style={{ width: '100%', padding: '12px', marginTop: 8 }}
          disabled={loading}>
          {loading ? 'Authenticating...' : 'Sign In'}
        </button>
      </form>
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SIDEBAR
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function Sidebar({ page, setPage, isOpen, setOpen, onLogout }: { page: Page; setPage: (p: Page) => void; isOpen: boolean; setOpen: (o: boolean) => void; onLogout: () => void }) {
  const items: { id: Page; icon: string; label: string }[] = [
    { id: 'dashboard', icon: '◉', label: 'Dashboard' },
    { id: 'signals', icon: '∿', label: 'Signals' },
    { id: 'watchlists', icon: '☰', label: 'Watchlists' },
    { id: 'scanners', icon: '⊛', label: 'Scanners' },
    { id: 'settings', icon: '⚙', label: 'Settings' },
  ];

  return (
    <>
      {isOpen && <div className="sidebar-overlay" style={{ backdropFilter: 'blur(4px)', backgroundColor: 'rgba(0, 0, 0, 0.4)' }} onClick={() => setOpen(false)}></div>}
      <aside className={`sidebar ${isOpen ? 'open' : ''}`} style={{ backgroundColor: 'rgba(18, 18, 18, 0.7)', backdropFilter: 'blur(20px)', borderRight: '1px solid rgba(255, 255, 255, 0.05)' }}
        onMouseLeave={() => setOpen(false)}>
        <div className="sidebar-brand">
          <img src="/assets/logo.svg" alt="TradeNexus Logo" className="sidebar-brand-icon" style={{ width: 44, height: 44, background: 'transparent' }} />
          <div className="sidebar-brand-text">TradeNexus</div>
          <button className="btn-icon" style={{ marginLeft: 'auto', border: 'none', background: 'transparent' }} onClick={() => setOpen(false)}>✕</button>
        </div>
        <nav className="sidebar-nav">
          {items.map(item => (
            <button key={item.id}
              className={`sidebar-item ${page === item.id ? 'active' : ''}`}
              onClick={() => { setPage(item.id); setOpen(false); }}>
              <span className="sidebar-item-icon">{item.icon}</span>
              {item.label}
            </button>
          ))}
        </nav>
        <div className="sidebar-footer">
          <button className="sidebar-item" onClick={onLogout}>
            <span className="sidebar-item-icon">↪</span>
            Sign Out
          </button>
        </div>
      </aside>
    </>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DASHBOARD
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function DashboardPage() {
  const [health, setHealth] = useState<SystemHealth | null>(null);
  const [signals, setSignals] = useState<Signal[]>([]);
  const [stats, setStats] = useState<Record<string, number>>({});

  const refresh = useCallback(async () => {
    try {
      const [h, s, st] = await Promise.all([api.getHealth(), api.getSignals(), api.getSignalStats()]);
      setHealth(h);
      setSignals(s || []);
      setStats(st);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { refresh(); const id = setInterval(refresh, 10000); return () => clearInterval(id); }, [refresh]);

  const uptime = health ? formatUptime(health.uptimeSeconds) : '--';
  const totalSignals = (stats['PINE_MOMENTUM'] || 0) + (stats['WEEKLY_CONSOLIDATED'] || 0);

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Dashboard</h1>
        <p className="page-subtitle">System overview and recent activity</p>
      </div>

      {health && (
        <div className="health-bar animate-in">
          <HealthDot label="WebSocket" ok={health.webSocketConnected} />
          <HealthDot label="MongoDB" ok={health.mongoConnected} />
          <HealthDot label="Redis" ok={health.redisConnected} />
          <HealthDot label="Market" ok={health.marketOpen} />
          <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-tertiary)' }}>
            Uptime: {uptime}
          </span>
        </div>
      )}

      <div className="stats-grid">
        <div className="stat-card animate-in">
          <div className="stat-label">Total Signals</div>
          <div className="stat-value">{totalSignals}</div>
        </div>
        <div className="stat-card animate-in">
          <div className="stat-label">Pine Momentum</div>
          <div className="stat-value">{stats['PINE_MOMENTUM'] || 0}</div>
        </div>
        <div className="stat-card animate-in">
          <div className="stat-label">Weekly Scanner</div>
          <div className="stat-value">{stats['WEEKLY_CONSOLIDATED'] || 0}</div>
        </div>
        <div className="stat-card animate-in">
          <div className="stat-label">Subscriptions</div>
          <div className="stat-value">{health?.activeSubscriptions || 0}</div>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <h3 className="card-title">Recent Signals</h3>
          <span className="card-label">{signals.length} latest</span>
        </div>
        {signals.length === 0 ? (
          <div className="empty-state">
            <div className="empty-state-icon">◬</div>
            <div className="empty-state-title">No signals yet</div>
            <div className="empty-state-desc">Add stocks to a watchlist and signals will appear here when detected.</div>
          </div>
        ) : (
          <div className="signals-grid">
            {signals.slice(0, 10).map(sig => <AwesomeSignalCard key={sig.signalHash} signal={sig} />)}
          </div>
        )}
      </div>
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SIGNALS PAGE
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function SignalsPage() {
  const [signals, setSignals] = useState<Signal[]>([]);
  const [filter, setFilter] = useState('');

  useEffect(() => {
    api.getSignals().then(s => setSignals(s || [])).catch(() => { });
  }, []);

  const filtered = filter
    ? signals.filter(s => s.timeframe === filter || s.category === filter)
    : signals;

  return (
    <div className="animate-in">
      <div className="page-header">
        <h1 className="page-title">Signals</h1>
        <p className="page-subtitle">All generated trading signals</p>
      </div>

      <div className="filter-scroll" style={{ display: 'flex', gap: 8, marginBottom: 24, overflowX: 'auto', paddingBottom: 8 }}>
        {['', '4H', '1D', '1W', '1M'].map(tf => (
          <button key={tf} className={`btn btn-sm ${filter === tf ? 'btn-primary' : 'btn-secondary'}`}
            style={{ borderRadius: 20, whiteSpace: 'nowrap' }}
            onClick={() => setFilter(tf)}>
            {tf || 'All Timeframes'}
          </button>
        ))}
      </div>

      {filtered.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">▤</div>
          <div className="empty-state-title">No signals found</div>
          <div className="empty-state-desc">Signals will appear here when detected by the strategy engine.</div>
        </div>
      ) : (
        <div className="signals-grid">
          {filtered.map(sig => <AwesomeSignalCard key={sig.signalHash} signal={sig} />)}
        </div>
      )}
    </div>
  );
}

function AwesomeSignalCard({ signal }: { signal: Signal }) {
  const isBuy = signal.signalType === 'BUY';
  const time = new Date(signal.createdAt).toLocaleString('en-IN', {
    day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit',
  });

  return (
    <div className={`awesome-signal-card ${isBuy ? 'buy' : 'sell'} animate-in`}>
      <div className="awesome-signal-badge-container">
        <span className={`signal-badge ${isBuy ? 'buy' : 'sell'}`}>
          {isBuy ? '▲ BUY' : '▼ SELL'}
        </span>
        <span className="tag tag-timeframe">{signal.timeframe}</span>
      </div>

      <div className="awesome-signal-title">{signal.symbol}</div>

      <div className="awesome-signal-price">₹{signal.price.toFixed(2)}</div>

      <div className="awesome-signal-metrics">
        <div className="metric">
          <span className="label">Conviction</span>
          <span className={`val ${signal.conviction.includes('HIGH') || signal.conviction.includes('MAX') ? 'highlight' : ''}`}>{signal.conviction.replace('_', ' ')}</span>
        </div>
        <div className="metric">
          <span className="label">RSI</span>
          <span className={`val ${signal.rsiValue > 60 ? 'green' : signal.rsiValue < 40 ? 'red' : ''}`}>{signal.rsiValue.toFixed(1)}</span>
        </div>
        <div className="metric">
          <span className="label">Volume</span>
          <span className="val">{signal.relativeVolume.toFixed(1)}x</span>
        </div>
      </div>

      {signal.breakoutReason && (
        <div className="awesome-signal-reason">
          {signal.breakoutReason}
        </div>
      )}

      <div className="awesome-signal-footer">
        {time}
      </div>
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// WATCHLISTS PAGE
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function WatchlistsPage() {
  const [watchlists, setWatchlists] = useState<Watchlist[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [showAddStock, setShowAddStock] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<Instrument[]>([]);
  const [expandedWatchlists, setExpandedWatchlists] = useState<Record<string, boolean>>({});
  const [stockPrices, setStockPrices] = useState<Record<string, number>>({});

  const loadWatchlists = useCallback(async () => {
    try {
      const wls = await api.getWatchlists();
      setWatchlists(wls || []);
      setExpandedWatchlists(prev => {
        const next: Record<string, boolean> = {};
        for (const wl of (wls || [])) {
          next[wl.id] = prev[wl.id] ?? false;
        }
        return next;
      });
    } catch { }
  }, []);

  useEffect(() => { loadWatchlists(); }, [loadWatchlists]);

  const handleCreate = async () => {
    if (!newName.trim()) return;
    await api.createWatchlist(newName.trim());
    setNewName('');
    setShowCreate(false);
    loadWatchlists();
  };

  const handleSearch = async (q: string) => {
    setSearchQuery(q);
    if (q.length >= 2) {
      const results = await api.searchSymbols(q);
      setSearchResults(results || []);
    } else {
      setSearchResults([]);
    }
  };

  const handleAddStock = async (wlId: string, inst: Instrument) => {
    await api.addStock(wlId, { symbol: inst.symbol, exchange: inst.exch_seg, name: inst.name });
    setShowAddStock(null);
    setSearchQuery('');
    setSearchResults([]);
    loadWatchlists();
  };

  const handleRemoveStock = async (wlId: string, symbol: string) => {
    await api.removeStock(wlId, symbol);
    loadWatchlists();
  };

  const handleDelete = (id: string) => {
    setConfirmDelete(id);
  };

  const handleDeleteConfirm = async () => {
    if (confirmDelete) {
      await api.deleteWatchlist(confirmDelete);
      setConfirmDelete(null);
      loadWatchlists();
    }
  };

  const watchlistTokens = useMemo(() => {
    const seen = new Set<string>();
    const tokens: string[] = [];
    for (const wl of watchlists) {
      for (const stock of wl.stocks || []) {
        if (!seen.has(stock.token)) {
          seen.add(stock.token);
          tokens.push(stock.token);
        }
      }
    }
    return tokens;
  }, [watchlists]);

  useEffect(() => {
    if (watchlistTokens.length === 0) {
      return;
    }

    const saved = localStorage.getItem('tn_auth');
    if (!saved) {
      return;
    }

    const { u, p } = JSON.parse(saved) as { u?: string; p?: string };
    if (!u || !p) {
      return;
    }

    let ws: WebSocket | null = null;
    let retryTimer: number | undefined;
    let closed = false;

    const connect = () => {
      const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
      const params = new URLSearchParams({
        u,
        p,
        tokens: watchlistTokens.join(','),
      });

      ws = new WebSocket(`${protocol}://${window.location.host}/api/quotes/ws?${params.toString()}`);
      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as QuoteWSMessage;
          if (data.type !== 'quotes' || !Array.isArray(data.quotes)) {
            return;
          }
          setStockPrices(prev => {
            const next = { ...prev };
            for (const q of data.quotes) {
              if (q.token && Number.isFinite(q.price)) {
                next[q.token] = q.price;
              }
            }
            return next;
          });
        } catch {
          // Ignore malformed ws messages
        }
      };

      ws.onclose = () => {
        if (!closed) {
          retryTimer = window.setTimeout(connect, 2000);
        }
      };
    };

    connect();

    return () => {
      closed = true;
      if (retryTimer) {
        window.clearTimeout(retryTimer);
      }
      if (ws) {
        ws.close();
      }
    };
  }, [watchlistTokens]);

  const toggleWatchlist = (id: string) => {
    setExpandedWatchlists(prev => ({ ...prev, [id]: !prev[id] }));
  };

  return (
    <div className="animate-in">
      <div className="page-header" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div>
          <h1 className="page-title">Watchlists</h1>
          <p className="page-subtitle">Manage stocks for signal monitoring</p>
        </div>
        <button className="btn btn-primary" style={{ borderRadius: 20 }} onClick={() => setShowCreate(true)}>+ New</button>
      </div>

      {watchlists.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">☰</div>
          <div className="empty-state-title">No watchlists</div>
          <div className="empty-state-desc">Create a watchlist and add stocks to start receiving signals.</div>
          <button className="btn btn-primary" onClick={() => setShowCreate(true)}>Create Watchlist</button>
        </div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
          {watchlists.map(wl => (
            <div key={wl.id} className="awesome-watchlist-card animate-in">
              <div className="awesome-watchlist-header">
                <div style={{ display: 'flex', alignItems: 'baseline', gap: 12 }}>
                  <h3 className="awesome-watchlist-title">{wl.name}</h3>
                  <span className="awesome-watchlist-count">{wl.stocks?.length || 0} stocks</span>
                </div>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button className="btn btn-sm btn-secondary" style={{ borderRadius: 20 }} onClick={() => toggleWatchlist(wl.id)}>
                    {expandedWatchlists[wl.id] ? 'Collapse' : 'Expand'}
                  </button>
                  <button className="btn btn-sm btn-secondary" style={{ borderRadius: 20 }} onClick={() => setShowAddStock(wl.id)}>+ Add Stock</button>
                  <button className="btn btn-sm btn-danger" style={{ borderRadius: 20 }} onClick={() => handleDelete(wl.id)}>Delete</button>
                </div>
              </div>

              {expandedWatchlists[wl.id] && wl.stocks?.length > 0 && (
                <div className="awesome-watchlist-grid">
                  {wl.stocks.map(stock => (
                    <div key={stock.symbol} className="awesome-watchlist-item">
                      <div className="info">
                        <div className="symbol">{stock.symbol}</div>
                        <div className="name">{stock.name}</div>
                      </div>
                      <div className="price-container">
                        <div className="price">
                          {stockPrices[stock.token] !== undefined ? `₹${stockPrices[stock.token].toFixed(2)}` : '--'}
                        </div>
                        <button className="btn-icon remove-btn" title="Remove" onClick={() => handleRemoveStock(wl.id, stock.symbol)}>×</button>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {!expandedWatchlists[wl.id] && wl.stocks?.length > 0 && (
                <div className="awesome-watchlist-preview-container">
                  {wl.stocks.slice(0, 4).map(stock => (
                    <div key={stock.token} className="awesome-watchlist-preview-item">
                      <span className="sym">{stock.symbol}</span>
                      <span className="prc">{stockPrices[stock.token] !== undefined ? `₹${stockPrices[stock.token].toFixed(2)}` : '--'}</span>
                    </div>
                  ))}
                  {wl.stocks.length > 4 && <div className="awesome-watchlist-preview-item more">+{wl.stocks.length - 4} more</div>}
                </div>
              )}

              {expandedWatchlists[wl.id] && wl.stocks?.length === 0 && (
                <div className="awesome-watchlist-preview-container" style={{ padding: '16px', justifyContent: 'center' }}>
                  <div className="name" style={{ color: 'var(--text-tertiary)' }}>No stocks yet. Add some to get started.</div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Create Modal */}
      {showCreate && (
        <div className="modal-overlay" onClick={() => setShowCreate(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h2 className="modal-title">New Watchlist</h2>
            <div className="input-group">
              <label className="input-label">Name</label>
              <input className="input" value={newName} onChange={e => setNewName(e.target.value)}
                placeholder="e.g., Nifty 50 Momentum" autoFocus />
            </div>
            <div className="modal-actions">
              <button className="btn btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
              <button className="btn btn-primary" onClick={handleCreate}>Create</button>
            </div>
          </div>
        </div>
      )}

      {/* Add Stock Modal */}
      {showAddStock && (
        <div className="modal-overlay" onClick={() => { setShowAddStock(null); setSearchResults([]); setSearchQuery(''); }}>
          <div className="modal" style={{ display: 'flex', flexDirection: 'column', maxHeight: '80vh' }} onClick={e => e.stopPropagation()}>
            <h2 className="modal-title" style={{ flexShrink: 0 }}>Add Stock</h2>
            <div className="input-group" style={{ flexShrink: 0 }}>
              <label className="input-label">Search</label>
              <input className="input" value={searchQuery} onChange={e => handleSearch(e.target.value)}
                placeholder="Search symbol or name..." autoFocus />
            </div>
            <div style={{ flex: 1, overflowY: 'auto', marginTop: 12, minHeight: 0 }}>
              {searchResults.map(inst => (
                <div key={inst.token} className="watchlist-stock" style={{ cursor: 'pointer' }}
                  onClick={() => handleAddStock(showAddStock, inst)}>
                  <div>
                    <div className="watchlist-stock-symbol">{inst.symbol}</div>
                    <div className="watchlist-stock-name">{inst.name} · {inst.exch_seg}</div>
                  </div>
                  <span style={{ color: 'var(--blue-primary)', fontSize: 13 }}>+ Add</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Delete Confirm Modal */}
      {confirmDelete && (
        <div className="modal-overlay" onClick={() => setConfirmDelete(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h2 className="modal-title">Delete Watchlist</h2>
            <p style={{ color: 'var(--text-secondary)', marginBottom: 24 }}>Are you sure you want to delete this watchlist? This action cannot be undone.</p>
            <div className="modal-actions" style={{ display: 'flex', gap: 12, justifyContent: 'flex-end' }}>
              <button className="btn btn-secondary" onClick={() => setConfirmDelete(null)}>Cancel</button>
              <button className="btn btn-danger" onClick={handleDeleteConfirm}>Delete</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SCANNERS PAGE
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function ScannersPage() {
  const [selectedScanner, setSelectedScanner] = useState<'PINE' | 'WEEKLY' | null>(null);

  if (selectedScanner === 'PINE') {
    return <PineScannerView onBack={() => setSelectedScanner(null)} />;
  }
  if (selectedScanner === 'WEEKLY') {
    return <WeeklyScannerView onBack={() => setSelectedScanner(null)} />;
  }

  return (
    <div className="animate-in">
      <div className="page-header">
        <h1 className="page-title">Scanners</h1>
        <p className="page-subtitle">List of pre-defined scanners</p>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 48 }}>

        {/* Pine Scanner Card */}
        <div className="scanner-container">
          <div className="scanner-card" onClick={() => setSelectedScanner('PINE')}>
            <div className="scanner-badge">REAL-TIME</div>
            <h2 className="scanner-card-title pine">Pine Scanner</h2>
            <p className="scanner-card-desc">Momentum and breakout signals from PineScript strategies.</p>
          </div>
          <div className="scanner-tags-container">
            <span className="scanner-tag-pill">Intraday</span>
            <span className="scanner-tag-pill">Swing</span>
            <span className="scanner-tag-pill">Momentum</span>
          </div>
        </div>

        {/* Weekly Scanner Card */}
        <div className="scanner-container">
          <div className="scanner-card" onClick={() => setSelectedScanner('WEEKLY')}>
            <div className="scanner-badge">MANUAL</div>
            <h2 className="scanner-card-title weekly">Weekly Scanner</h2>
            <p className="scanner-card-desc">Manual weekly preview using the current available daily candles.</p>
          </div>
          <div className="scanner-tags-container">
            <span className="scanner-tag-pill">Weekly Breakout</span>
            <span className="scanner-tag-pill">52 Week High</span>
            <span className="scanner-tag-pill">Price Action</span>
          </div>
        </div>

      </div>
    </div>
  );
}

function PineScannerView({ onBack }: { onBack: () => void }) {
  const [signals, setSignals] = useState<Signal[]>([]);
  const [filter, setFilter] = useState('');

  useEffect(() => {
    api.getSignals().then(s => {
      const pineSignals = (s || []).filter(sig => sig.category && sig.category.includes('PINE'));
      setSignals(pineSignals);
    }).catch(() => { });
  }, []);

  const filtered = filter
    ? signals.filter(s => s.timeframe === filter)
    : signals;

  return (
    <div className="animate-in">
      <div className="page-header" style={{ display: 'flex', alignItems: 'flex-start', flexDirection: 'column' }}>
        <button className="btn btn-sm btn-secondary" onClick={onBack} style={{ marginBottom: 16 }}>← Back to Scanners</button>
        <h1 className="page-title">Pine Scanner</h1>
        <p className="page-subtitle">Real-time signals from PineScript momentum strategies</p>
      </div>

      <div style={{ display: 'flex', gap: 8, marginBottom: 24 }}>
        {['', '4H', '1D', '1W', '1M'].map(tf => (
          <button key={tf} className={`btn btn-sm ${filter === tf ? 'btn-primary' : 'btn-secondary'}`}
            onClick={() => setFilter(tf)}>
            {tf || 'All Timeframes'}
          </button>
        ))}
      </div>

      {filtered.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">▤</div>
          <div className="empty-state-title">No signals found</div>
          <div className="empty-state-desc">Signals will appear here when detected by the strategy engine.</div>
        </div>
      ) : (
        <div className="signals-grid">
          {filtered.map(sig => <AwesomeSignalCard key={sig.signalHash} signal={sig} />)}
        </div>
      )}
    </div>
  );
}

function WeeklyScannerView({ onBack }: { onBack: () => void }) {
  const [results, setResults] = useState<any[]>([]);
  const [triggering, setTriggering] = useState(false);

  useEffect(() => {
    api.getScannerResults().then(r => setResults(r || [])).catch(() => { });
  }, []);

  const handleTrigger = async () => {
    setTriggering(true);
    try {
      await api.triggerScanner();
      setTimeout(() => {
        api.getScannerResults().then(r => setResults(r || [])).catch(() => { });
        setTriggering(false);
      }, 3000);
    } catch { setTriggering(false); }
  };

  return (
    <div className="animate-in">
      <div className="page-header" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div>
          <button className="btn btn-sm btn-secondary" onClick={onBack} style={{ marginBottom: 16 }}>← Back to Scanners</button>
          <h1 className="page-title">Weekly Scanner</h1>
          <p className="page-subtitle">Manual weekly preview using the current available daily candles</p>
        </div>
        <button className="btn btn-primary" onClick={handleTrigger} disabled={triggering}>
          {triggering ? 'Scanning...' : 'Run Manual Scan'}
        </button>
      </div>

      <div className="stats-grid">
        {['WEEKLY_BREAKOUT', 'WEEKLY_CONTINUATION', 'WEEKLY_52WK_HIGH', 'WEEKLY_PRICE_ACTION'].map(type => (
          <div key={type} className="stat-card animate-in">
            <div className="stat-label">{type.replace('WEEKLY_', '').replace(/_/g, ' ')}</div>
            <div className="stat-value">{results.filter(r => r.scannerType === type).length}</div>
            <div className="stat-detail">manual matches</div>
          </div>
        ))}
      </div>

      {results.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">⊛</div>
          <div className="empty-state-title">No scanner results</div>
          <div className="empty-state-desc">Run the manual weekly scanner to preview current-week matches.</div>
        </div>
      ) : (
        <div className="card">
          <div className="card-header">
            <h3 className="card-title">Scanner Matches</h3>
            <span className="card-label">{results.length} manual results</span>
          </div>
          <div className="signals-grid">
            {results.map((r, i) => (
              <div key={i} className="awesome-signal-card buy">
                <div className="awesome-signal-badge-container">
                  <span className="signal-badge buy">▲ {r.scannerType?.replace('WEEKLY_', '').replace(/_/g, ' ')}</span>
                  {r.isPartialWeek && <span className="tag tag-timeframe">PARTIAL WEEK</span>}
                </div>

                <div className="awesome-signal-title">{r.symbol}</div>
                <div className="awesome-signal-price">₹{r.closePrice?.toFixed(2)}</div>

                <div className="awesome-signal-metrics" style={{ gridTemplateColumns: '1fr 1fr' }}>
                  <div className="metric">
                    <span className="label">Volume</span>
                    <span className="val">{formatNumber(r.volume)}</span>
                  </div>
                  <div className="metric">
                    <span className="label">Exchange</span>
                    <span className="val">{r.exchange}</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SETTINGS PAGE
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
function SettingsPage() {
  const [telegramSettings, setTelegramSettings] = useState<TelegramSettings | null>(null);
  const [botToken, setBotToken] = useState('');
  const [chatId, setChatId] = useState('');
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    api.getTelegramSettings().then(s => setTelegramSettings(s)).catch(() => { });
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setMessage('');
    try {
      await api.saveTelegramSettings(botToken, chatId);
      setMessage('Telegram settings saved');
      setTelegramSettings({ isConfigured: true, testSuccess: false });
    } catch (e: any) {
      setMessage('Error: ' + e.message);
    }
    setSaving(false);
  };

  const handleTest = async () => {
    setTesting(true);
    setMessage('');
    try {
      await api.testTelegram(botToken, chatId);
      setMessage('Test message sent successfully');
    } catch (e: any) {
      setMessage('Error: ' + e.message);
    }
    setTesting(false);
  };

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Settings</h1>
        <p className="page-subtitle">Configure integrations and preferences</p>
      </div>

      <div className="card" style={{ maxWidth: 560 }}>
        <div className="card-header">
          <h3 className="card-title">Telegram Integration</h3>
          {telegramSettings?.isConfigured && (
            <span className="signal-badge buy">Configured</span>
          )}
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div className="input-group">
            <label className="input-label">Bot Token</label>
            <input className="input" type="password" value={botToken}
              onChange={e => setBotToken(e.target.value)} placeholder="Enter your Telegram bot token" />
          </div>
          <div className="input-group">
            <label className="input-label">Chat ID</label>
            <input className="input" value={chatId}
              onChange={e => setChatId(e.target.value)} placeholder="Enter your chat or group ID" />
          </div>
          {message && (
            <div style={{ fontSize: 13, color: !message.startsWith('Error') ? 'var(--green-primary)' : 'var(--red-primary)' }}>
              {message}
            </div>
          )}
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn btn-secondary" onClick={handleTest} disabled={testing || !botToken || !chatId}>
              {testing ? 'Sending...' : 'Test Connection'}
            </button>
            <button className="btn btn-primary" onClick={handleSave} disabled={saving || !botToken || !chatId}>
              {saving ? 'Saving...' : 'Save Settings'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SHARED COMPONENTS
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


function HealthDot({ label, ok }: { label: string; ok: boolean }) {
  return (
    <div className="health-item">
      <span className={`status-dot ${ok ? 'online' : 'offline'}`} />
      {label}
    </div>
  );
}

function formatUptime(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatNumber(value?: number): string {
  return new Intl.NumberFormat('en-IN').format(value || 0);
}
