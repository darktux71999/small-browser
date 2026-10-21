//! Per-tab navigation history.
//!
//! Model only: the UI and document swapping are wired elsewhere. Semantics
//! follow the browser rule that pushing a new entry after going back drops the
//! forward entries.

#[derive(Debug, Clone)]
pub struct History {
    entries: Vec<String>,
    index: usize,
}

impl History {
    pub fn new(initial: impl Into<String>) -> History {
        History {
            entries: vec![initial.into()],
            index: 0,
        }
    }

    pub fn current(&self) -> &str {
        &self.entries[self.index]
    }

    pub fn len(&self) -> usize {
        self.entries.len()
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    pub fn can_go_back(&self) -> bool {
        self.index > 0
    }

    pub fn can_go_forward(&self) -> bool {
        self.index + 1 < self.entries.len()
    }

    /// Record a navigation. No-op when it targets the current entry.
    pub fn push(&mut self, url: impl Into<String>) {
        let url = url.into();
        if url == self.entries[self.index] {
            return;
        }
        self.entries.truncate(self.index + 1);
        self.entries.push(url);
        self.index = self.entries.len() - 1;
    }

    /// Move back one entry; returns the URL to load, if any.
    pub fn back(&mut self) -> Option<&str> {
        if !self.can_go_back() {
            return None;
        }
        self.index -= 1;
        Some(self.current())
    }

    /// Move forward one entry; returns the URL to load, if any.
    pub fn forward(&mut self) -> Option<&str> {
        if !self.can_go_forward() {
            return None;
        }
        self.index += 1;
        Some(self.current())
    }
}

#[cfg(test)]
mod tests {
    use super::History;

    #[test]
    fn back_and_forward_walk_entries() {
        let mut h = History::new("a");
        h.push("b");
        h.push("c");
        assert_eq!(h.current(), "c");
        assert_eq!(h.back(), Some("b"));
        assert_eq!(h.back(), Some("a"));
        assert_eq!(h.back(), None);
        assert_eq!(h.forward(), Some("b"));
        assert_eq!(h.forward(), Some("c"));
        assert_eq!(h.forward(), None);
    }

    #[test]
    fn push_after_back_drops_forward_entries() {
        let mut h = History::new("a");
        h.push("b");
        h.push("c");
        h.back();
        h.push("d");
        assert_eq!(h.len(), 3);
        assert_eq!(h.current(), "d");
        assert!(!h.can_go_forward());
        assert_eq!(h.back(), Some("b"));
    }

    #[test]
    fn pushing_current_is_a_noop() {
        let mut h = History::new("a");
        h.push("a");
        assert_eq!(h.len(), 1);
    }
}
