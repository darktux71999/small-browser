//! browser-lite: lightweight desktop browser built on the system stack.
//! Modules are public so tests and future embedders reuse the same engine.

pub mod cookies;
pub mod css;
pub mod css_libcss;
pub mod dom;
pub mod history;
pub mod js;
pub mod layout;
pub mod net;
pub mod paint;
pub mod sandbox;
pub mod shell;
pub mod storage;
pub mod sys;
pub mod tabs;
pub mod url;
