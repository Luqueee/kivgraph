//! This crate loads. Its repository also holds a workspace the analyzer cannot
//! read, so every answer about this repository is a lower bound: the crate over
//! there declares everything in the source and nothing in the graph.

/// Declared where the index can read it.
pub fn rust_visible() -> &'static str {
    "visible"
}
