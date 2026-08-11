//! The provider crate of the cross-repository fixture.

/// Value is the type the consumer builds.
pub struct Value {
    pub inner: i32,
}

/// double is the exported function the consumer calls.
pub fn double(value: i32) -> i32 {
    value * 2
}
