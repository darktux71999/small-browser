//! Navigation requests produced by page clicks.
//!
//! Blitz calls a `NavigationProvider` when a link is activated. The provider
//! only enqueues the request; the application drains it on the event loop,
//! loads the target and swaps the document of the owning window.

use std::sync::{Arc, Mutex};

use blitz_traits::navigation::{NavigationOptions, NavigationProvider};

#[derive(Debug, Clone)]
pub struct NavRequest {
    pub source_document: usize,
    pub url: String,
}

#[derive(Debug, Clone, Default)]
pub struct NavQueue(Arc<Mutex<Vec<NavRequest>>>);

impl NavQueue {
    pub fn new() -> NavQueue {
        NavQueue::default()
    }

    pub fn drain(&self) -> Vec<NavRequest> {
        match self.0.lock() {
            Ok(mut queue) => std::mem::take(&mut *queue),
            Err(_) => Vec::new(),
        }
    }
}

pub struct QueueNavigationProvider {
    pub queue: NavQueue,
}

impl NavigationProvider for QueueNavigationProvider {
    fn navigate_to(&self, options: NavigationOptions) {
        if let Ok(mut queue) = self.queue.0.lock() {
            queue.push(NavRequest {
                source_document: options.source_document,
                url: options.url.to_string(),
            });
        }
    }
}
