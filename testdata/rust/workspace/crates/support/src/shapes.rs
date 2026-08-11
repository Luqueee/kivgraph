//! Traits and implementations: the structural relations of Rust.

/// Named is the supertrait every drawable shape answers.
pub trait Named {
    fn name(&self) -> &'static str;
}

/// Drawable extends Named, which is an EXTENDS relation.
pub trait Drawable: Named {
    fn draw(&self) -> i32;
}

/// Circle implements both traits, which are two IMPLEMENTS relations.
pub struct Circle {
    pub radius: i32,
}

impl Named for Circle {
    fn name(&self) -> &'static str {
        "circle"
    }
}

impl Drawable for Circle {
    fn draw(&self) -> i32 {
        self.radius
    }
}

impl Circle {
    /// new is inherent: it overrides nothing. `Self` names the impl block,
    /// which SCIP mentions and never defines.
    pub fn new(radius: i32) -> Self {
        Self { radius }
    }
}
