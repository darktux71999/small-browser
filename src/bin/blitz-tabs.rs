//! Tabbed browser: one window per tab, keyboard shortcuts, link navigation
//! and per-tab history.
//!
//! Shortcuts: Ctrl+T (new), Ctrl+W (close), Ctrl+Tab / Ctrl+Shift+Tab (cycle),
//! Alt+Left / Alt+Right (history).

use std::collections::HashMap;
use std::process::ExitCode;
use std::sync::Arc;

use anyrender_vello_cpu::VelloCpuWindowRenderer;
use blitz_shell::{
    create_default_event_loop, BlitzApplication, BlitzShellEvent, View, WindowConfig,
};
use winit::application::ApplicationHandler;
use winit::event::{ElementState, WindowEvent};
use winit::event_loop::{ActiveEventLoop, EventLoopProxy};
use winit::keyboard::{Key, ModifiersState, NamedKey};
use winit::window::WindowId;

use browser_lite::nav::{NavQueue, QueueNavigationProvider};
use browser_lite::tabs::Tabs;
use browser_lite::{blitz_engine, net, url};

const NEW_TAB_HTML: &str = r#"<!doctype html>
<html><head><style>
body { margin: 0; font-family: sans-serif; background: #fafafa; color: #222; padding: 32px; }
h1 { color: #0b5; }
</style></head><body><h1>New tab</h1><p>Ctrl+T new · Ctrl+W close · Ctrl+Tab cycle</p></body></html>"#;

struct TabbedApp {
    app: BlitzApplication<VelloCpuWindowRenderer>,
    proxy: EventLoopProxy<BlitzShellEvent>,
    tabs: Tabs,
    by_window: HashMap<WindowId, u64>,
    by_doc: HashMap<usize, u64>,
    mods: ModifiersState,
    queue: NavQueue,
    initial: Vec<String>,
    started: bool,
}

impl TabbedApp {
    fn load(target: &str) -> Result<String, String> {
        if target == "about:blank" {
            return Ok(NEW_TAB_HTML.to_string());
        }
        if url::is_http(target) {
            net::fetch(target).map(|bytes| String::from_utf8_lossy(&bytes).into_owned())
        } else {
            std::fs::read_to_string(target).map_err(|e| format!("{target}: {e}"))
        }
    }

    fn base_url(target: &str) -> Option<String> {
        if url::is_http(target) {
            Some(target.to_string())
        } else if target == "about:blank" {
            None
        } else {
            url::file_url(target)
        }
    }

    fn build(&self, target: &str) -> Result<Box<dyn blitz_dom::Document>, String> {
        let html = Self::load(target)?;
        let provider = Arc::new(QueueNavigationProvider {
            queue: self.queue.clone(),
        });
        Ok(blitz_engine::build_document_with(
            &html,
            Self::base_url(target),
            Some(provider),
        ))
    }

    fn open_tab(&mut self, event_loop: &ActiveEventLoop, target: &str) {
        let doc = match self.build(target) {
            Ok(doc) => doc,
            Err(err) => {
                eprintln!("blitz-tabs: {err}");
                return;
            }
        };
        let doc_id = doc.id();
        let tab_id = self.tabs.open(target);
        let config = WindowConfig::new(doc, VelloCpuWindowRenderer::new());
        let mut view = View::init(config, event_loop, &self.proxy);
        view.resume();
        let window_id = view.window_id();
        self.app.windows.insert(window_id, view);
        self.by_window.insert(window_id, tab_id);
        self.by_doc.insert(doc_id, tab_id);
    }

    fn close_window(&mut self, event_loop: &ActiveEventLoop, window_id: WindowId) {
        if let Some(view) = self.app.windows.remove(&window_id) {
            self.by_doc.remove(&view.doc.id());
            if let Some(tab_id) = self.by_window.remove(&window_id) {
                self.tabs.close(tab_id);
            }
            drop(view);
        }
        if self.app.windows.is_empty() {
            event_loop.exit();
        }
    }

    fn window_for_tab(&self, tab_id: u64) -> Option<WindowId> {
        self.by_window
            .iter()
            .find(|(_, id)| **id == tab_id)
            .map(|(window, _)| *window)
    }

    fn focus_tab(&self, tab_id: u64) {
        if let Some(window_id) = self.window_for_tab(tab_id) {
            if let Some(view) = self.app.windows.get(&window_id) {
                view.window.focus_window();
                view.request_redraw();
            }
        }
    }

    fn cycle(&mut self, forward: bool) {
        if self.tabs.is_empty() {
            return;
        }
        if forward {
            self.tabs.next();
        } else {
            self.tabs.prev();
        }
        if let Some(tab) = self.tabs.active() {
            let id = tab.id;
            self.focus_tab(id);
        }
    }

    /// Replace the document of `window_id` and record the navigation when
    /// `push` is set (back/forward already moved the history cursor).
    fn load_into(&mut self, window_id: WindowId, tab_id: u64, target: &str, push: bool) {
        let doc = match self.build(target) {
            Ok(doc) => doc,
            Err(err) => {
                eprintln!("blitz-tabs: {err}");
                return;
            }
        };
        let new_doc_id = doc.id();
        let old = self
            .app
            .windows
            .get(&window_id)
            .map(|view| (view.doc.id(), view.doc.viewport().clone()));
        let Some((old_doc_id, viewport)) = old else {
            return;
        };
        if let Some(view) = self.app.windows.get_mut(&window_id) {
            view.doc = doc;
            view.doc.set_viewport(viewport);
            if let Some(title) = view.doc.find_title_node().map(|node| node.text_content()) {
                view.window.set_title(&title);
            }
            view.request_redraw();
        }
        self.by_doc.remove(&old_doc_id);
        self.by_doc.insert(new_doc_id, tab_id);
        if push {
            if let Some(tab) = self.tabs.get_mut(tab_id) {
                tab.history.push(target.to_string());
            }
        }
    }

    fn history_move(&mut self, window_id: WindowId, back: bool) {
        let Some(&tab_id) = self.by_window.get(&window_id) else {
            return;
        };
        let target = match self.tabs.get_mut(tab_id) {
            Some(tab) => {
                if back {
                    tab.history.back().map(str::to_string)
                } else {
                    tab.history.forward().map(str::to_string)
                }
            }
            None => None,
        };
        if let Some(target) = target {
            self.load_into(window_id, tab_id, &target, false);
        }
    }

    fn handle_shortcut(&mut self, event_loop: &ActiveEventLoop, window_id: WindowId, event: &WindowEvent) -> bool {
        let WindowEvent::KeyboardInput { event, .. } = event else {
            return false;
        };
        if event.state != ElementState::Pressed {
            return false;
        }

        if self.mods.control_key() {
            match &event.logical_key {
                Key::Character(c) if c.as_str().eq_ignore_ascii_case("t") => {
                    self.open_tab(event_loop, "about:blank");
                    return true;
                }
                Key::Character(c) if c.as_str().eq_ignore_ascii_case("w") => {
                    self.close_window(event_loop, window_id);
                    return true;
                }
                Key::Named(NamedKey::Tab) => {
                    self.cycle(!self.mods.shift_key());
                    return true;
                }
                _ => {}
            }
        }

        if self.mods.alt_key() {
            match &event.logical_key {
                Key::Named(NamedKey::ArrowLeft) => {
                    self.history_move(window_id, true);
                    return true;
                }
                Key::Named(NamedKey::ArrowRight) => {
                    self.history_move(window_id, false);
                    return true;
                }
                _ => {}
            }
        }
        false
    }
}

impl ApplicationHandler<BlitzShellEvent> for TabbedApp {
    fn resumed(&mut self, event_loop: &ActiveEventLoop) {
        if !self.started {
            self.started = true;
            let targets = std::mem::take(&mut self.initial);
            for target in &targets {
                self.open_tab(event_loop, target);
            }
        } else {
            for view in self.app.windows.values_mut() {
                view.resume();
            }
        }
    }

    fn suspended(&mut self, event_loop: &ActiveEventLoop) {
        self.app.suspended(event_loop);
    }

    fn window_event(
        &mut self,
        event_loop: &ActiveEventLoop,
        window_id: WindowId,
        event: WindowEvent,
    ) {
        if let WindowEvent::ModifiersChanged(modifiers) = &event {
            self.mods = modifiers.state();
            return;
        }
        if matches!(event, WindowEvent::CloseRequested) {
            self.close_window(event_loop, window_id);
            return;
        }
        if self.handle_shortcut(event_loop, window_id, &event) {
            return;
        }
        self.app.window_event(event_loop, window_id, event);
    }

    fn user_event(&mut self, event_loop: &ActiveEventLoop, event: BlitzShellEvent) {
        self.app.user_event(event_loop, event);
    }

    fn about_to_wait(&mut self, event_loop: &ActiveEventLoop) {
        let requests = self.queue.drain();
        for request in requests {
            let Some(&tab_id) = self.by_doc.get(&request.source_document) else {
                continue;
            };
            let Some(window_id) = self.window_for_tab(tab_id) else {
                continue;
            };
            self.load_into(window_id, tab_id, &request.url, true);
        }
        let _ = event_loop;
    }
}

fn main() -> ExitCode {
    let mut targets: Vec<String> = std::env::args().skip(1).collect();
    if targets.is_empty() {
        targets.push("about:blank".to_string());
    }

    let event_loop = create_default_event_loop();
    let proxy = event_loop.create_proxy();
    let app = BlitzApplication::new(proxy.clone());

    let mut tabbed = TabbedApp {
        app,
        proxy,
        tabs: Tabs::new(),
        by_window: HashMap::new(),
        by_doc: HashMap::new(),
        mods: ModifiersState::empty(),
        queue: NavQueue::new(),
        initial: targets,
        started: false,
    };

    match event_loop.run_app(&mut tabbed) {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("blitz-tabs: {err}");
            ExitCode::from(1)
        }
    }
}
