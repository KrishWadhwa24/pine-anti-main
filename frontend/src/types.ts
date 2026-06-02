// TradeNexus API types

export interface SystemHealth {
  webSocketConnected: boolean;
  webSocketLastTick: string;
  mongoConnected: boolean;
  redisConnected: boolean;
  telegramConfigured: boolean;
  activeSubscriptions: number;
  uptimeSeconds: number;
  lastSignalAt: string;
  lastRecoveryAt: string;
  marketOpen: boolean;
}

export interface Signal {
  id: string;
  signalHash: string;
  symbol: string;
  exchange: string;
  timeframe: string;
  signalType: 'BUY' | 'SELL';
  category: string;
  conviction: string;
  candleTimestamp: string;
  price: number;
  breakoutReason: string;
  trendConfirm: string;
  volumeConfirm: string;
  rsiState: string;
  relativeVolume: number;
  rsiValue: number;
  atrValue: number;
  bodyStrength: number;
  matchedScanners?: string[];
  convictionScore?: number;
  telegramSent: boolean;
  telegramSentAt?: string;
  createdAt: string;
}

export interface Watchlist {
  id: string;
  name: string;
  stocks: Stock[];
  isActive: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface Stock {
  symbol: string;
  token: string;
  exchange: string;
  exchangeType: number;
  name: string;
}

export interface Instrument {
  token: string;
  symbol: string;
  name: string;
  exch_seg: string;
}

export interface ScannerMatch {
  id: string;
  symbol: string;
  exchange: string;
  scannerType: string;
  weekTimestamp: string;
  matched: boolean;
  scannerMode?: string;
  isPartialWeek?: boolean;
  closePrice: number;
  volume: number;
  rsi: number;
  reason: string;
  createdAt: string;
}

export interface TelegramSettings {
  isConfigured: boolean;
  lastTestedAt?: string;
  testSuccess: boolean;
}
