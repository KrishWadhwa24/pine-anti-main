const fs = require('fs');
const path = './frontend/src/App.tsx';
let code = fs.readFileSync(path, 'utf8');

// Brand Icons
code = code.replace(/<div className="sidebar-brand-icon"[^>]*>⚡<\/div>/g, match => match.replace('⚡', 'TN'));
code = code.replace(/<div className="login-brand-icon">⚡<\/div>/g, '<div className="login-brand-icon">TN</div>');
code = code.replace(/<div className="sidebar-brand-icon">⚡<\/div>/g, '<div className="sidebar-brand-icon">TN</div>');

// Sidebar / Nav Icons
code = code.replace(/icon: '⚡'/g, "icon: '∿'");
code = code.replace(/<span className="mobile-nav-icon">⚡<\/span>/g, '<span className="mobile-nav-icon">∿</span>');

// Landing Page Pills
code = code.replace(/💰 100% Free Forever/g, '100% Free Forever');
code = code.replace(/📱 Real-time Telegram Alerts/g, 'Real-time Telegram Alerts');
code = code.replace(/⚡ PineScript Engine/g, 'PineScript Engine');

// Empty States
code = code.replace(/<div className="empty-state-icon">⚡<\/div>/g, '<div className="empty-state-icon">◬</div>');
code = code.replace(/<div className="empty-state-icon">📊<\/div>/g, '<div className="empty-state-icon">▤</div>');

// Settings Page
code = code.replace(/<h3 className="card-title">📱 Telegram Integration<\/h3>/g, '<h3 className="card-title">Telegram Integration</h3>');
code = code.replace(/✅ Telegram settings saved/g, 'Telegram settings saved');
code = code.replace(/✅ Test message sent successfully/g, 'Test message sent successfully');
code = code.replace(/❌ ' \+ e.message/g, "Error: ' + e.message");
code = code.replace(/message\.startsWith\('✅'\)/g, "!message.startsWith('Error')");

fs.writeFileSync(path, code, 'utf8');
