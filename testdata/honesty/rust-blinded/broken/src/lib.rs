//! This crate belongs to a workspace Cargo cannot resolve, which is how a Rust
//! repository ends up with a scope the index could not read at all. The source
//! declares these names; the graph holds none of them.

/// Invisible to any load of this repository.
pub fn rust_hidden() -> &'static str {
    "hidden"
}

/// Shares its name with nothing else in the corpus, so a lookup for it finds no
/// declaration while the source has one.
pub fn rust_shadow() -> &'static str {
    rust_hidden()
}
