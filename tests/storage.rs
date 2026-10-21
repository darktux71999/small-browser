//! T5 — storage: history and cookies over SQLite.

use browser_lite::storage::Store;

#[test]
fn push_history_then_read_most_recent_first() {
    let store = Store::open_memory().expect("open in-memory store");

    store.push_history("https://a.example/", "A").expect("push A");
    store.push_history("https://b.example/", "B").expect("push B");
    store.push_history("https://c.example/", "C").expect("push C");

    let recent = store.recent_history(2).expect("read history");
    assert_eq!(recent.len(), 2);
    assert_eq!(recent[0].url, "https://c.example/");
    assert_eq!(recent[0].title, "C");
    assert_eq!(recent[1].url, "https://b.example/");
}

#[test]
fn clear_history_empties_the_table() {
    let store = Store::open_memory().expect("open in-memory store");
    store.push_history("https://a.example/", "A").expect("push A");

    store.clear_history().expect("clear history");

    let recent = store.recent_history(10).expect("read history");
    assert!(recent.is_empty());
}

#[test]
fn cookies_round_trip_per_domain() {
    let store = Store::open_memory().expect("open in-memory store");

    store.set_cookie("a.example", "sid", "123").expect("set sid");
    store.set_cookie("a.example", "lang", "es").expect("set lang");
    store.set_cookie("b.example", "sid", "999").expect("set sid on b");

    let a = store.cookies_for("a.example").expect("cookies for a");
    assert_eq!(
        a,
        vec![
            ("lang".to_string(), "es".to_string()),
            ("sid".to_string(), "123".to_string())
        ]
    );

    let b = store.cookies_for("b.example").expect("cookies for b");
    assert_eq!(b, vec![("sid".to_string(), "999".to_string())]);

    let none = store.cookies_for("c.example").expect("cookies for c");
    assert!(none.is_empty());
}

#[test]
fn setting_the_same_cookie_twice_replaces_the_value() {
    let store = Store::open_memory().expect("open in-memory store");
    store.set_cookie("a.example", "sid", "1").expect("set sid");
    store.set_cookie("a.example", "sid", "2").expect("replace sid");

    let cookies = store.cookies_for("a.example").expect("cookies");
    assert_eq!(cookies, vec![("sid".to_string(), "2".to_string())]);
}

#[test]
fn open_creates_the_schema_and_stores_data_on_disk() {
    let dir = std::env::temp_dir().join(format!("bl-storage-{}", std::process::id()));
    std::fs::create_dir_all(&dir).expect("create temp dir");
    let path = dir.join("store.sqlite");
    let _ = std::fs::remove_file(&path);

    {
        let store = Store::open(path.to_str().expect("utf-8 path")).expect("open store");
        store.push_history("https://a.example/", "A").expect("push A");
    }

    let store = Store::open(path.to_str().expect("utf-8 path")).expect("reopen store");
    let recent = store.recent_history(10).expect("read history");
    assert_eq!(recent.len(), 1);
    assert_eq!(recent[0].url, "https://a.example/");

    let _ = std::fs::remove_file(&path);
    let _ = std::fs::remove_dir(&dir);
}

#[test]
fn open_reports_an_error_instead_of_panicking() {
    let missing = "/nonexistent-browser-lite-dir/store.sqlite";
    assert!(Store::open(missing).is_err());
}
