//! Tab model: ordered tabs, one active tab, per-tab history.
//!
//! Rendering and window wiring live in the binary; this module stays pure so
//! the semantics are testable.

use crate::history::History;

#[derive(Debug)]
pub struct Tab {
    pub id: u64,
    pub title: String,
    pub history: History,
}

impl Tab {
    pub fn url(&self) -> &str {
        self.history.current()
    }
}

#[derive(Debug)]
pub struct Tabs {
    tabs: Vec<Tab>,
    active: usize,
    next_id: u64,
}

impl Default for Tabs {
    fn default() -> Self {
        Tabs::new()
    }
}

impl Tabs {
    pub fn new() -> Tabs {
        Tabs {
            tabs: Vec::new(),
            active: 0,
            next_id: 1,
        }
    }

    pub fn open(&mut self, url: impl Into<String>) -> u64 {
        let id = self.next_id;
        self.next_id += 1;
        let url = url.into();
        self.tabs.push(Tab {
            id,
            title: String::new(),
            history: History::new(url),
        });
        self.active = self.tabs.len() - 1;
        id
    }

    pub fn close(&mut self, id: u64) -> bool {
        let Some(index) = self.tabs.iter().position(|t| t.id == id) else {
            return false;
        };
        self.tabs.remove(index);
        if self.tabs.is_empty() {
            self.active = 0;
        } else if self.active >= self.tabs.len() {
            self.active = self.tabs.len() - 1;
        } else if index < self.active {
            self.active -= 1;
        }
        true
    }

    pub fn activate(&mut self, id: u64) -> bool {
        match self.tabs.iter().position(|t| t.id == id) {
            Some(index) => {
                self.active = index;
                true
            }
            None => false,
        }
    }

    pub fn next(&mut self) {
        if !self.tabs.is_empty() {
            self.active = (self.active + 1) % self.tabs.len();
        }
    }

    pub fn prev(&mut self) {
        if !self.tabs.is_empty() {
            self.active = (self.active + self.tabs.len() - 1) % self.tabs.len();
        }
    }

    pub fn active(&self) -> Option<&Tab> {
        self.tabs.get(self.active)
    }

    pub fn active_mut(&mut self) -> Option<&mut Tab> {
        self.tabs.get_mut(self.active)
    }

    pub fn get(&self, id: u64) -> Option<&Tab> {
        self.tabs.iter().find(|t| t.id == id)
    }

    pub fn get_mut(&mut self, id: u64) -> Option<&mut Tab> {
        self.tabs.iter_mut().find(|t| t.id == id)
    }

    pub fn len(&self) -> usize {
        self.tabs.len()
    }

    pub fn is_empty(&self) -> bool {
        self.tabs.is_empty()
    }

    pub fn iter(&self) -> impl Iterator<Item = &Tab> {
        self.tabs.iter()
    }
}

#[cfg(test)]
mod tests {
    use super::Tabs;

    #[test]
    fn opening_tabs_activates_the_new_one() {
        let mut tabs = Tabs::new();
        let a = tabs.open("a");
        let b = tabs.open("b");
        assert_eq!(tabs.len(), 2);
        assert_eq!(tabs.active().unwrap().id, b);
        assert!(tabs.activate(a));
        assert_eq!(tabs.active().unwrap().id, a);
    }

    #[test]
    fn closing_keeps_a_valid_active_tab() {
        let mut tabs = Tabs::new();
        let a = tabs.open("a");
        let b = tabs.open("b");
        assert!(tabs.close(b));
        assert_eq!(tabs.active().unwrap().id, a);
        assert!(tabs.close(a));
        assert!(tabs.active().is_none());
        assert!(tabs.is_empty());
    }

    #[test]
    fn cycling_wraps() {
        let mut tabs = Tabs::new();
        let a = tabs.open("a");
        let b = tabs.open("b");
        tabs.next();
        assert_eq!(tabs.active().unwrap().id, a);
        tabs.prev();
        assert_eq!(tabs.active().unwrap().id, b);
    }

    #[test]
    fn each_tab_keeps_its_own_history() {
        let mut tabs = Tabs::new();
        tabs.open("a");
        tabs.open("b");
        tabs.active_mut().unwrap().history.push("c");
        assert_eq!(tabs.active().unwrap().history.current(), "c");
        tabs.prev();
        assert_eq!(tabs.active().unwrap().history.current(), "a");
    }
}
