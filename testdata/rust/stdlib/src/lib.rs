//! Fixture for the standard library as a synthetic provider.
//!
//! Every item here exists to exercise one thing the graph is silent about while
//! `core`, `alloc` and `std` have no identity: a derive, an operator that
//! resolves to a trait method of `core`, the `?` operator, and a plain call
//! into the standard library.

use std::num::ParseIntError;
use std::ops::Add;

/// Derives three traits declared by `core`. Nothing in the expansion is written
/// here, so without the standard library in the graph the relation cannot exist.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct Offset {
    pub line: u32,
    pub column: u32,
}

/// Implements a trait of `core` for a local type. The `impl` names the trait and
/// the associated type, both declared outside this repository.
impl Add for Offset {
    type Output = Offset;

    fn add(self, other: Offset) -> Offset {
        Offset {
            line: self.line + other.line,
            column: self.column + other.column,
        }
    }
}

/// Uses the operator, which the analyzer attributes to `core::ops::Add::add`
/// rather than to the `impl` above.
pub fn shift(origin: Offset, delta: Offset) -> Offset {
    origin + delta
}

/// The `?` operator desugars into `core::ops::Try::branch`, so the call it makes
/// is invisible without the standard library.
pub fn parse_line(text: &str) -> Result<u32, ParseIntError> {
    let line: u32 = text.trim().parse()?;
    Ok(line)
}

/// A plain call into the standard library, which is the common case: the graph
/// today loses every one of them.
pub fn render(offset: &Offset) -> String {
    let mut out = String::with_capacity(8);
    out.push_str(&offset.line.to_string());
    out.push(':');
    out.push_str(&offset.column.to_string());
    out
}

/// Clones through the derived implementation, so the call has a target only when
/// the derive produced one.
pub fn duplicate(offset: &Offset) -> Offset {
    offset.clone()
}
