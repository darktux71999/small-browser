//! SQLite-backed persistence for browsing history and cookies.
//!
//! Model only: no UI integration. The store owns its schema and reports
//! every failure as `Err` instead of panicking, so a broken database never
//! takes the browser down.

use rusqlite::{params, Connection};

const SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    visited_at INTEGER NOT NULL DEFAULT (CAST(strftime('%s', 'now') AS INTEGER))
);
CREATE TABLE IF NOT EXISTS cookies (
    domain TEXT NOT NULL,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '/',
    host_only INTEGER NOT NULL DEFAULT 1,
    secure INTEGER NOT NULL DEFAULT 0,
    http_only INTEGER NOT NULL DEFAULT 0,
    expires INTEGER,
    PRIMARY KEY (domain, name, path)
);
"#;

/// One visited page.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryEntry {
    pub url: String,
    pub title: String,
}

/// A cookie as it is kept in the database.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StoredCookie {
    pub domain: String,
    pub name: String,
    pub value: String,
    pub path: String,
    pub host_only: bool,
    pub secure: bool,
    pub http_only: bool,
    /// Absolute expiry, in seconds since the Unix epoch.
    pub expires: Option<i64>,
}

/// A store over one SQLite database.
pub struct Store {
    conn: Connection,
}

impl Store {
    /// Open (creating it if needed) the database at `path` and ensure the
    /// schema exists.
    pub fn open(path: &str) -> Result<Store, String> {
        let conn = Connection::open(path).map_err(|e| format!("open {path}: {e}"))?;
        Store::with_schema(conn, path)
    }

    /// Open a private in-memory database. Used by tests.
    pub fn open_memory() -> Result<Store, String> {
        let conn =
            Connection::open_in_memory().map_err(|e| format!("open in-memory database: {e}"))?;
        Store::with_schema(conn, ":memory:")
    }

    fn with_schema(conn: Connection, label: &str) -> Result<Store, String> {
        migrate(&conn).map_err(|e| format!("migrate {label}: {e}"))?;
        conn.execute_batch(SCHEMA)
            .map_err(|e| format!("create schema in {label}: {e}"))?;
        Ok(Store { conn })
    }

    /// Record a visited page.
    pub fn push_history(&self, url: &str, title: &str) -> Result<(), String> {
        self.conn
            .execute(
                "INSERT INTO history (url, title) VALUES (?1, ?2)",
                params![url, title],
            )
            .map(|_| ())
            .map_err(|e| format!("push history for {url}: {e}"))
    }

    /// The most recently visited pages, newest first, at most `limit` of them.
    pub fn recent_history(&self, limit: usize) -> Result<Vec<HistoryEntry>, String> {
        let limit = limit.min(i64::MAX as usize) as i64;
        let mut stmt = self
            .conn
            .prepare("SELECT url, title FROM history ORDER BY id DESC LIMIT ?1")
            .map_err(|e| format!("prepare history query: {e}"))?;
        let rows = stmt
            .query_map(params![limit], |row| {
                Ok(HistoryEntry {
                    url: row.get(0)?,
                    title: row.get(1)?,
                })
            })
            .map_err(|e| format!("query history: {e}"))?;
        let mut out = Vec::new();
        for row in rows {
            out.push(row.map_err(|e| format!("read history row: {e}"))?);
        }
        Ok(out)
    }

    /// Drop every history entry.
    pub fn clear_history(&self) -> Result<(), String> {
        self.conn
            .execute("DELETE FROM history", [])
            .map(|_| ())
            .map_err(|e| format!("clear history: {e}"))
    }

    /// Store a cookie for `domain`, replacing any previous value of `name`.
    pub fn set_cookie(&self, domain: &str, name: &str, value: &str) -> Result<(), String> {
        self.conn
            .execute(
                "INSERT INTO cookies (domain, name, value, path) VALUES (?1, ?2, ?3, '/')
                 ON CONFLICT(domain, name, path) DO UPDATE SET value = excluded.value",
                params![domain, name, value],
            )
            .map(|_| ())
            .map_err(|e| format!("set cookie {name} for {domain}: {e}"))
    }

    /// The cookies of one domain, as `(name, value)` pairs ordered by name.
    pub fn cookies_for(&self, domain: &str) -> Result<Vec<(String, String)>, String> {
        let mut stmt = self
            .conn
            .prepare("SELECT name, value FROM cookies WHERE domain = ?1 ORDER BY name")
            .map_err(|e| format!("prepare cookie query: {e}"))?;
        let rows = stmt
            .query_map(params![domain], |row| Ok((row.get(0)?, row.get(1)?)))
            .map_err(|e| format!("query cookies for {domain}: {e}"))?;
        let mut out = Vec::new();
        for row in rows {
            out.push(row.map_err(|e| format!("read cookie row: {e}"))?);
        }
        Ok(out)
    }

    /// Persist a cookie with all of its attributes.
    pub fn store_cookie(&self, cookie: &StoredCookie) -> Result<(), String> {
        self.conn
            .execute(
                "INSERT INTO cookies (domain, name, value, path, host_only, secure, http_only, expires)
                 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)
                 ON CONFLICT(domain, name, path) DO UPDATE SET
                     value = excluded.value,
                     host_only = excluded.host_only,
                     secure = excluded.secure,
                     http_only = excluded.http_only,
                     expires = excluded.expires",
                params![
                    cookie.domain,
                    cookie.name,
                    cookie.value,
                    cookie.path,
                    cookie.host_only as i64,
                    cookie.secure as i64,
                    cookie.http_only as i64,
                    cookie.expires,
                ],
            )
            .map(|_| ())
            .map_err(|e| {
                format!(
                    "store cookie {} for {}: {e}",
                    cookie.name, cookie.domain
                )
            })
    }

    /// Every stored cookie, ordered by domain, path and name.
    pub fn load_cookies(&self) -> Result<Vec<StoredCookie>, String> {
        let mut stmt = self
            .conn
            .prepare(
                "SELECT domain, name, value, path, host_only, secure, http_only, expires
                 FROM cookies ORDER BY domain, path, name",
            )
            .map_err(|e| format!("prepare cookie query: {e}"))?;
        let rows = stmt
            .query_map([], |row| {
                Ok(StoredCookie {
                    domain: row.get(0)?,
                    name: row.get(1)?,
                    value: row.get(2)?,
                    path: row.get(3)?,
                    host_only: row.get::<_, i64>(4)? != 0,
                    secure: row.get::<_, i64>(5)? != 0,
                    http_only: row.get::<_, i64>(6)? != 0,
                    expires: row.get(7)?,
                })
            })
            .map_err(|e| format!("query cookies: {e}"))?;
        let mut out = Vec::new();
        for row in rows {
            out.push(row.map_err(|e| format!("read cookie row: {e}"))?);
        }
        Ok(out)
    }

    /// Drop every stored cookie.
    pub fn clear_cookies(&self) -> Result<(), String> {
        self.conn
            .execute("DELETE FROM cookies", [])
            .map(|_| ())
            .map_err(|e| format!("clear cookies: {e}"))
    }
}

/// Bring an older database up to the current schema.
///
/// The cookie table grew the `path`, `host_only`, `secure`, `http_only` and
/// `expires` columns; an old table is dropped and recreated, since cookies
/// can always be fetched again.
fn migrate(conn: &Connection) -> Result<(), String> {
    let tables: i64 = conn
        .query_row(
            "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'cookies'",
            [],
            |row| row.get(0),
        )
        .map_err(|e| format!("inspect schema: {e}"))?;
    if tables == 0 {
        return Ok(());
    }
    let mut stmt = conn
        .prepare("PRAGMA table_info(cookies)")
        .map_err(|e| format!("inspect cookies table: {e}"))?;
    let columns = stmt
        .query_map([], |row| row.get::<_, String>(1))
        .map_err(|e| format!("read cookies columns: {e}"))?;
    let mut names = Vec::new();
    for column in columns {
        names.push(column.map_err(|e| format!("read cookies column: {e}"))?);
    }
    if !names.iter().any(|name| name == "path") {
        conn.execute("DROP TABLE cookies", [])
            .map_err(|e| format!("drop old cookies table: {e}"))?;
    }
    Ok(())
}
