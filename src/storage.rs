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
    PRIMARY KEY (domain, name)
);
"#;

/// One visited page.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryEntry {
    pub url: String,
    pub title: String,
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
                "INSERT INTO cookies (domain, name, value) VALUES (?1, ?2, ?3)
                 ON CONFLICT(domain, name) DO UPDATE SET value = excluded.value",
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
}
