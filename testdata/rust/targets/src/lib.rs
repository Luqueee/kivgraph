//! The library target: the only crate of this package another repository can
//! name.

pub mod engine;

use vendored::Seed;

/// Runs the engine over a seed the vendored crate builds.
pub fn run() -> u32 {
    engine::start(Seed::new(7))
}
