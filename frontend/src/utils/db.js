import initSqlJs from 'sql.js';

let db = null;
const DB_STORAGE_KEY = 'stargate_db';

/**
 * Robustly converts Uint8Array to Base64 string
 */
function uint8ArrayToBase64(arr) {
  let binary = '';
  const len = arr.byteLength;
  for (let i = 0; i < len; i++) {
    binary += String.fromCharCode(arr[i]);
  }
  return btoa(binary);
}

/**
 * Robustly converts Base64 string to Uint8Array
 */
function base64ToUint8Array(base64) {
  const binaryString = atob(base64);
  const len = binaryString.length;
  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return bytes;
}

export async function initDB() {
  if (db) return db;

  try {
    const SQL = await initSqlJs({
      // Serve from local public folder to avoid CORS and MIME type issues
      locateFile: file => `/${file}`
    });

    const savedDB = localStorage.getItem(DB_STORAGE_KEY);
    if (savedDB) {
      const uint8Array = base64ToUint8Array(savedDB);
      db = new SQL.Database(uint8Array);
    } else {
      db = new SQL.Database();
      db.run(`
        CREATE TABLE IF NOT EXISTS auth_state (
          id INTEGER PRIMARY KEY CHECK (id = 1),
          api_key TEXT,
          wallet TEXT,
          email TEXT
        );
        CREATE TABLE IF NOT EXISTS wallet_keys (
          wallet TEXT PRIMARY KEY,
          api_key TEXT,
          email TEXT,
          last_used INTEGER
        );
      `);
      // Initialize with old localStorage data if available for migration
      const oldKey = localStorage.getItem('X-API-Key');
      const oldWallet = localStorage.getItem('X-Wallet-Address');
      const oldEmail = localStorage.getItem('X-User-Email');
      if (oldKey) {
        db.run("INSERT OR REPLACE INTO auth_state (id, api_key, wallet, email) VALUES (1, ?, ?, ?)", 
          [oldKey, oldWallet || '', oldEmail || '']);
      }
      
      const oldMap = localStorage.getItem('X-Wallet-Key-Map');
      if (oldMap) {
        try {
          const parsed = JSON.parse(oldMap);
          for (const [wallet, data] of Object.entries(parsed)) {
            db.run("INSERT OR REPLACE INTO wallet_keys (wallet, api_key, email, last_used) VALUES (?, ?, ?, ?)",
              [wallet, data.apiKey, data.email || '', data.lastUsed || Date.now()]);
          }
        } catch (e) {
          console.error("Failed to migrate wallet map", e);
        }
      }
      saveDB();
    }
    return db;
  } catch (err) {
    console.error("Failed to initialize sql.js", err);
    // Fallback or rethrow? 
    // For now we rethrow to let AuthContext handle it.
    throw err;
  }
}

export function saveDB() {
  if (!db) return;
  const data = db.export();
  const base64 = uint8ArrayToBase64(data);
  localStorage.setItem(DB_STORAGE_KEY, base64);
}

export function getAuthState() {
  if (!db) return { apiKey: '', wallet: '', email: '' };
  const res = db.exec("SELECT api_key, wallet, email FROM auth_state WHERE id = 1");
  if (res.length > 0 && res[0].values.length > 0) {
    const [apiKey, wallet, email] = res[0].values[0];
    return { apiKey: apiKey || '', wallet: wallet || '', email: email || '' };
  }
  return { apiKey: '', wallet: '', email: '' };
}

export function setAuthState(apiKey, wallet, email) {
  if (!db) return;
  console.log("Setting auth state in DB", { wallet, email });
  db.run("INSERT OR REPLACE INTO auth_state (id, api_key, wallet, email) VALUES (1, ?, ?, ?)", [apiKey, wallet, email]);
  saveDB();
}

export function clearAuthState() {
  if (!db) return;
  db.run("DELETE FROM auth_state WHERE id = 1");
  saveDB();
}

export function getWalletKeysFromDB() {
  if (!db) return {};
  const res = db.exec("SELECT wallet, api_key, email, last_used FROM wallet_keys");
  const keys = {};
  if (res.length > 0) {
    res[0].values.forEach(([wallet, apiKey, email, lastUsed]) => {
      keys[wallet] = { apiKey, email, lastUsed };
    });
  }
  return keys;
}

export function updateWalletKeyInDB(wallet, apiKey, email) {
  if (!db) return;
  db.run("INSERT OR REPLACE INTO wallet_keys (wallet, api_key, email, last_used) VALUES (?, ?, ?, ?)", 
    [wallet, apiKey, email, Date.now()]);
  saveDB();
}

export function removeWalletKeyFromDB(wallet) {
  if (!db) return;
  db.run("DELETE FROM wallet_keys WHERE wallet = ?", [wallet]);
  saveDB();
}

export function removeStaleKeysFromDB(thresholdMs) {
  if (!db) return;
  const now = Date.now();
  db.run("DELETE FROM wallet_keys WHERE ? - last_used > ?", [now, thresholdMs]);
  saveDB();
}
