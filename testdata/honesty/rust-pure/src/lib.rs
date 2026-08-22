//! This crate has nothing the index cannot read. It is the control of the Rust
//! arm: every answer about it must be COMPLETE, or the verdict is a constant
//! rather than a measurement.

/// Reached from this crate and from nowhere else.
pub fn rust_reachable() -> &'static str {
    "reachable"
}

/// The one use of `rust_reachable`.
pub fn rust_caller() -> &'static str {
    rust_reachable()
}

/// Declared and never used, so an answer about it is a real absence.
pub fn rust_lonely() -> &'static str {
    "lonely"
}
