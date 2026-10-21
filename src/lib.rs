//! browser-lite: lightweight desktop browser built on the system stack.
//! Modules are public so tests and future embedders reuse the same engine.

pub mod css;
pub mod dom;
pub mod history;
pub mod js;
pub mod layout;
pub mod nav;
pub mod net;
pub mod paint;
pub mod shell;
pub mod sys;
pub mod tabs;
pub mod url;

#[cfg(feature = "blitz-engine")]
pub mod blitz_engine;
