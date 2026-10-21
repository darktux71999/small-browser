//! browser-lite: lightweight desktop browser built on the system stack.
//! Modules are public so tests and future embedders reuse the same engine.

pub mod css;
pub mod dom;
pub mod js;
pub mod layout;
pub mod paint;
pub mod shell;
pub mod sys;

#[cfg(feature = "blitz-engine")]
pub mod blitz_engine;
